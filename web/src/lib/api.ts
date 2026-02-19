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
        // Ignore user-initiated cancellations (typeahead AbortController, route changes, etc.)
        // Otherwise we incorrectly flip the global "server unavailable" banner during normal UX.
        if (axios.isCancel(error) || error?.code === 'ERR_CANCELED' || error?.name === 'CanceledError' || error?.name === 'AbortError') {
          return Promise.reject(error);
        }

        const url = String(error?.config?.url || '');

        // Check if it's a network error (CORS, server down, etc.)
        if (!error.response) {
          // Network error - no response from server
          import('../stores/serverStatusStore').then(({ useServerStatusStore }) => {
            useServerStatusStore.getState().setServerUnavailable('Server is temporarily unavailable. Please try again later.');
          }).catch(() => {});
        } else if (error.response.status >= 500) {
          // Best-effort endpoints (autocomplete/prefetch) shouldn't mark the whole server as unavailable.
          if (url.includes('/api/horizon/entities/search')) {
            return Promise.reject(error);
          }
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

  async getHorizonTelemetry(params: {
    component: string;
    metric: string;
    limit?: number;
  }): Promise<{
    items: Array<{
      component: string;
      metric: string;
      windowStart: string;
      windowSizeSec: number;
      dimensions?: Record<string, string>;
      counters: Record<string, number>;
      updatedAt: string;
    }>;
    summary: Record<string, number>;
    hitRate: number;
    tenantId: string;
  }> {
    const response = await this.client.get(`/api/v1/admin/horizon/telemetry`, { params });
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
      rss?: string[];
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

  async attachSource(projectId: string, sourceId: string, status?: string): Promise<{ id: string }> {
    const response = await this.client.post<{ id: string }>(`/api/v1/projects/${projectId}/sources`, {
      sourceId,
      status,
    });
    return response.data;
  }

  async detachSource(projectId: string, sourceId: string): Promise<{ success: boolean }> {
    const response = await this.client.delete<{ success: boolean }>(`/api/v1/projects/${projectId}/sources/${sourceId}`);
    return response.data;
  }

  async updateSourceProjectStatus(projectId: string, sourceId: string, status: 'active' | 'paused'): Promise<{ id: string; status: string }> {
    const response = await this.client.patch<{ id: string; status: string }>(`/api/v1/projects/${projectId}/sources/${sourceId}/status`, {
      status,
    });
    return response.data;
  }

  async getSourceDocuments(sourceId: string, limit?: number): Promise<any[]> {
    const params = limit ? `?limit=${limit}` : '';
    const response = await this.client.get<any[]>(`/api/v1/sources/${sourceId}/documents${params}`);
    return response.data;
  }

  async previewTelegramChannel(channel: string): Promise<{ channel: string; count: number; messages: any[] }> {
    const response = await this.client.get<{ channel: string; count: number; messages: any[] }>(`/api/v1/sources/preview/telegram?channel=${encodeURIComponent(channel)}`);
    return response.data;
  }

  /**
   * Trigger ingest for a specific source
   * POST /api/v1/sources/{id}/ingest
   */
  async ingestSource(sourceId: string): Promise<{ queued: boolean; sourceId: string; kind: string }> {
    const response = await this.client.post<{ queued: boolean; sourceId: string; kind: string }>(
      `/api/v1/sources/${sourceId}/ingest`
    );
    return response.data;
  }

  /**
   * Reprocess documents with status "embedded" for a source
   * POST /api/v1/sources/{id}/reprocess
   */
  async reprocessSource(sourceId: string): Promise<{ queued: boolean; sourceId: string; count: number; message: string }> {
    const response = await this.client.post<{ queued: boolean; sourceId: string; count: number; message: string }>(
      `/api/v1/sources/${sourceId}/reprocess`
    );
    return response.data;
  }

  async getSourceIngestHistory(sourceId: string, limit?: number): Promise<{ sourceId: string; history: any[]; count: number }> {
    const params = limit ? { limit } : {};
    const response = await this.client.get<{ sourceId: string; history: any[]; count: number }>(
      `/api/v1/sources/${sourceId}/ingest-history`,
      { params }
    );
    return response.data;
  }

  async getProjectInteractions(projectId: string): Promise<{ id: string; kind: string; content: string; createdAt: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any>; metadata?: { pinned?: boolean }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; coreDocIds?: string[] }[]> {
    const response = await this.client.get<{ items: { id: string; kind: string; content: string; createdAt: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; evaluations?: Record<string, any>; metadata?: { pinned?: boolean }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; coreDocIds?: string[] }[] }>(`/api/v1/projects/${projectId}/interactions`);
    return response.data.items;
  }

  async createProjectInteraction(projectId: string, data: { kind: 'user'|'assistant'|'system'|'note'|'section_start'|'section_end'|'action'|'research'|'ontology'; content: string; sectionId?: string; summary?: string; model?: string; usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number }; author?: { participantId: string; kind: string; displayName?: string }; recipients?: string[]; replyTo?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> }; originalMessageId?: string; subkind?: string; contentSanitized?: string; relations?: Array<{ type: string; targetId: string }>; language?: string; systemInstructionApplied?: string; generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean }; highRisk?: { level: string; score: number; rationale?: string; triggers?: string[] }; roles?: Array<{ role: string; confidence: number; rationale: string }>; industries?: Array<{ industry: string; confidence: number; rationale: string }>; evaluations?: Record<string, any>; coreDocIds?: string[]; metadata?: { pinned?: boolean }; researchQuery?: string; researchProgress?: { stage: 'planning'|'searching'|'generating'; status: 'in_progress'|'completed'; subQueries?: string[]; sourcesCount?: number }; subQueries?: string[]; researchSources?: Array<{ title: string; url: string; score?: number }>; ontologyEntityId?: string }): Promise<{ id: string }> {
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
      kind: 'user'|'assistant'|'system'|'note'|'section_start'|'section_end'|'action'|'ontology';
      content: string;
      summary?: string;
      model?: string;
      usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number };
      subkind?: string;
      ontologyEntityId?: string;
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
      roles?: Array<{ role: string; confidence: number; rationale: string }>;
      industries?: Array<{ industry: string; confidence: number; rationale: string }>;
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
    messages?: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>;
    systemInstruction?: string;
    temperature?: number;
    maxOutputTokens?: number;
    webSearchEnabled?: boolean;
    toolCallingEnabled?: boolean;
    researchEnabled?: boolean;
    strictJSONEnabled?: boolean;
    jsonSchema?: any;
    tools?: Array<{ name: string; description?: string; parameters: any }>;
    toolChoice?: 'auto'|'required';
    parallelToolCalls?: boolean;
    projectId?: string;
    streamId?: string;
  }, signal?: AbortSignal): Promise<{ content?: string; model: string; usage?: { promptTokens: number; outputTokens: number; totalTokens: number }; sources?: string[]; attributions?: Array<{ title?: string; url?: string }>; rawJSON?: string; }>
  {
    const body: any = {
      model: data.model,
      prompt: data.prompt,
      systemInstruction: data.systemInstruction,
      temperature: data.temperature,
      maxOutputTokens: data.maxOutputTokens,
      webSearchEnabled: !!data.webSearchEnabled,
      toolCallingEnabled: !!data.toolCallingEnabled,
      researchEnabled: !!data.researchEnabled,
      strictJSONEnabled: !!data.strictJSONEnabled,
      jsonSchema: data.jsonSchema,
      tools: data.tools,
      toolChoice: data.toolChoice,
      parallelToolCalls: data.parallelToolCalls,
    };
    // Add message history if provided (for conversation context)
    if (data.messages && data.messages.length > 0) {
      body.messages = data.messages;
    }
    // Add projectId and streamId for WebSocket events (used in Research mode)
    if (data.projectId) {
      body.projectId = data.projectId;
    }
    if (data.streamId) {
      body.streamId = data.streamId;
    }
    const response = await this.client.post(`/api/v1/genai/generate`, body, {
      signal,
    });
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

  // --- Share (public access) ---
  async shareCreate(projectId: string, expiresAtISO?: string): Promise<{ shareToken: string; expiresAt: string; url: string }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/share`, expiresAtISO ? { expiresAt: expiresAtISO } : {});
    return res.data as any;
  }

  async shareRevoke(projectId: string, token: string): Promise<{ success: boolean }> {
    const res = await this.client.delete(`/api/v1/projects/${projectId}/share/${token}`);
    return res.data as any;
  }

  async shareGetPublic(token: string): Promise<{
    v: '1';
    share: { tokenId: string; isActive: boolean; expiresAt: string; createdAt: string };
    project: { id: string; displayName: string; description?: string|null; createdAt?: string };
    interactions: Array<{
      id: string;
      kind: 'user'|'assistant';
      subkind?: string;
      replyTo?: string;
      content?: string;
      contentSanitized?: string;
      graphData?: { chartTitle: string; chartType?: 'line'|'bar'|'area'|'stackedBar'|'stackedArea'|'composed'; table: (string|number)[][]; sources: string[] };
      searchQueries?: string[];
      sources?: string[];
      createdAt: string;
      author?: { kind: 'human'|'ai'; displayName?: string };
      metadata?: { pinned?: boolean; notes?: string[] };
    }>;
    paging?: { nextCursor?: string; limit?: number };
  }> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/share/${encodeURIComponent(token)}`);
    return res.data as any;
  }

  async shareListDocs(token: string): Promise<{
    documents: Array<{
      id: string;
      filename: string;
      contentType: string;
      description?: string;
      sizeBytes?: number;
      docType?: string;
      createdAt: string;
      updatedAt: string;
    }>;
  }> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/share/${encodeURIComponent(token)}/docs`);
    return res.data as any;
  }

  async shareGetDoc(token: string, docId: string): Promise<{
    id: string;
    filename: string;
    contentType: string;
    description?: string;
    sizeBytes?: number;
    docType?: string;
    content: string;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/share/${encodeURIComponent(token)}/docs/${encodeURIComponent(docId)}`);
    return res.data as any;
  }

  async shareDownloadDoc(token: string, docId: string): Promise<Blob> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/share/${encodeURIComponent(token)}/docs/${encodeURIComponent(docId)}/download`, {
      responseType: 'blob',
    });
    return res.data;
  }

  async shareExportDocToWord(token: string, docId: string): Promise<Blob> {
    const res = await axios.post(`${API_BASE_URL}/api/v1/public/share/${encodeURIComponent(token)}/docs/${encodeURIComponent(docId)}/export`, {}, {
      responseType: 'blob',
    });
    return res.data;
  }

  // --- Verified URL Analysis ---
  async verifiedCreate(url: string, model?: string): Promise<{ code: string; url: string; status: string }> {
    // Use proxy endpoint (Cloud Function) instead of direct backend call
    // The proxy adds x-functions-key header for system authentication
    const proxyUrl = import.meta.env.VITE_VERIFIED_PROXY_URL;
    if (!proxyUrl) {
      throw new Error('VITE_VERIFIED_PROXY_URL is not configured. Please set it in environment variables.');
    }
    const payload: { url: string; model?: string } = { url };
    if (model) {
      payload.model = model;
    }
    const res = await axios.post(`${proxyUrl}/verified`, payload);
    return res.data as any;
  }

  async verifiedGetPublic(code: string): Promise<{
    id: string;
    code: string;
    url: string;
    canonicalUrl: string;
    analysis: string;
    model: string;
    status: string;
    documentId?: string;
    sourceId?: string;
    error?: string;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/verified/${encodeURIComponent(code)}`);
    return res.data as any;
  }

  async publicDocumentGet(documentId: string): Promise<{
    id: string;
    title?: string;
    url?: string;
    fullText: string;
    contentType?: string;
    documentType?: string;
    language?: string;
    ingestedAt: string;
  }> {
    const res = await axios.get(`${API_BASE_URL}/api/v1/public/documents/${encodeURIComponent(documentId)}`);
    return res.data as any;
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
    researchEnabled?: boolean;
  }, signal?: AbortSignal) {
    // Extract researchEnabled from options if present and add to top level
    const body: any = { ...data };
    if (data.options?.researchEnabled !== undefined) {
      body.researchEnabled = data.options.researchEnabled;
    }
    // Fire-and-forget HTTP 200 triggers Pub/Sub/WS stream events
    const response = await this.client.post(`/api/v1/genai/stream`, body, {
      signal,
    });
    return response.data;
  }

  // --- Core Docs (project attachments) ---
  async docsList(projectId: string, excludeDocumentType?: string) {
    const params = excludeDocumentType ? `?excludeDocumentType=${encodeURIComponent(excludeDocumentType)}` : '';
    const res = await this.client.get(`/api/v1/projects/${projectId}/docs${params}`);
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

  async docsUpdate(projectId: string, docId: string, data: { filename?: string; description?: string }) {
    // Encode docId to handle special characters like colons (e.g., Firestore version suffixes)
    const encodedDocId = encodeURIComponent(docId);
    const res = await this.client.patch(`/api/v1/projects/${projectId}/docs/${encodedDocId}`, data);
    return res.data as any;
  }

  async docsCheckIndexStatus(projectId: string, docId: string): Promise<{ indexed: boolean; reason?: string; error?: string }> {
    const res = await this.client.get(`/api/v1/projects/${projectId}/docs/${docId}/index-status`);
    return res.data as { indexed: boolean; reason?: string; error?: string };
  }

  async docsCheckBigQueryStatus(projectId: string, docId: string): Promise<{ available: boolean; reason?: string; error?: string }> {
    const res = await this.client.get(`/api/v1/projects/${projectId}/docs/${docId}/bigquery-status`);
    return res.data as { available: boolean; reason?: string; error?: string };
  }

  async docsCheckVertexSearchAvailability(projectId: string, docId: string): Promise<{ available: boolean; reason?: string; error?: string }> {
    const res = await this.client.get(`/api/v1/projects/${projectId}/docs/${docId}/vertex-search-availability`);
    return res.data as { available: boolean; reason?: string; error?: string };
  }

  // Prepare mode: Stage 1 - Analyze documents and generate commands
  async prepareAnalyze(projectId: string, userQuery: string): Promise<{
    commands: Array<{
      action: string;
      documentId: string;
      prompt: string;
      reason: string;
      priority?: number;
    }>;
    summary: string;
  }> {
    const response = await this.client.post(`/api/v1/projects/${projectId}/prepare/analyze`, {
      userQuery,
    });
    return response.data;
  }

  // Prepare mode: Stage 2 - Apply modifications to a document
  async prepareApply(projectId: string, documentId: string, prompt: string): Promise<{
    success: boolean;
    message: string;
    documentId?: string;
    error?: string;
  }> {
    const response = await this.client.post(`/api/v1/projects/${projectId}/prepare/apply`, {
      documentId,
      prompt,
    });
    return response.data;
  }

  async getBigQuerySchema(projectId: string): Promise<{
    projectID: string;
    datasetID: string;
    schemaInfo: string;
    tenantID: string;
    projectIDFilter: string;
  }> {
    console.log(`[API] getBigQuerySchema called: projectId=${projectId}`);
    try {
      const res = await this.client.get(`/api/v1/projects/${projectId}/analytics/schema`);
      console.log(`[API] getBigQuerySchema response:`, res.data);
      return res.data;
    } catch (error: any) {
      console.error(`[API] getBigQuerySchema error:`, error);
      throw error;
    }
  }

  async generateSQL(projectId: string, query: string): Promise<{
    sql: string;
    explanation?: string;
  }> {
    console.log(`[API] generateSQL called: projectId=${projectId}, query=${query}`);
    try {
      // Get schema info first
      const schemaData = await this.getBigQuerySchema(projectId);
      
      // Build prompt for SQL generation
      const filterInstructions = `\n\nCRITICAL FILTERING REQUIREMENTS:
- ALWAYS filter by tenantId = '${schemaData.tenantID}' when querying core_docs_metadata or documents_metadata tables
- ALWAYS filter by projectId = '${schemaData.projectIDFilter}' when querying core_docs_metadata table
- These filters MUST be included in WHERE clauses to ensure data isolation
- Example: WHERE tenantId = '${schemaData.tenantID}' AND projectId = '${schemaData.projectIDFilter}'`;

      const prompt = `You are a BigQuery SQL expert. Convert natural language to valid BigQuery SQL.

PROJECT: ${schemaData.projectID}
DATASET: ${schemaData.datasetID}

SCHEMA:
${schemaData.schemaInfo}

AVAILABLE DATA:
- core_docs_metadata: Document metadata (id, filename, contentType, storagePath, extractedText, parseMeta, createdAt, etc.)
- documents_metadata: RAG document metadata
- Files in GCS accessible via storagePath field
- BigQuery can read CSV, JSON, Parquet, Avro directly from GCS using EXTERNAL TABLE
${filterInstructions}
USER QUERY:
${query}

GENERATE SQL:
- Use \`${schemaData.projectID}.${schemaData.datasetID}.table_name\` format
- Filter by tenantId = '${schemaData.tenantID}' AND projectId = '${schemaData.projectIDFilter}' when querying core_docs_metadata
- For file content: use extractedText OR create EXTERNAL TABLE with format options
- For ML/forecasting: use CREATE MODEL with model_type (ARIMA_PLUS for time series)
- IMPORTANT: For TIMESTAMP fields, use DATE_TRUNC(timestamp_field, MONTH) or EXTRACT(YEAR FROM timestamp_field), EXTRACT(MONTH FROM timestamp_field) - NOT TIMESTAMP_TRUNC with MONTH
- For DATE fields, use DATE_TRUNC(date_field, MONTH)
- ARIMA_PLUS REQUIREMENTS (CRITICAL):
  * Minimum 2-3 data points per time series (preferably 10+)
  * Data must be ordered by timestamp
  * If grouping by categories (time_series_id_col), ensure each category has sufficient data points
  * If data is sparse, aggregate to fewer categories or use a single time series
  * For forecasting, prefer single time series unless user explicitly asks for multiple series
  * Use time_series_id_col ONLY if each category has at least 5-10 data points
- If you need to create a model AND query it, use TWO statements separated by semicolon: CREATE MODEL ... AS SELECT ...; SELECT ... FROM ML.FORECAST(...);
- Return ONLY SQL, no markdown code blocks, no explanations, no trailing backticks
- Use BigQuery features flexibly

Return ONLY the SQL query text, nothing else. No markdown, no backticks.`;

      // Generate SQL using genaiGenerate
      const genaiResponse = await this.genaiGenerate({
        model: 'gemini_quality',
        prompt,
        systemInstruction: 'You are a BigQuery SQL expert. Generate only valid SQL queries without any explanations or markdown formatting. Handle complex queries with multiple CTEs, JOINs, and subqueries. Always optimize for performance by using appropriate filters and limits.',
        temperature: 0.1,
        maxOutputTokens: 4096,
        webSearchEnabled: false,
        toolCallingEnabled: false,
        strictJSONEnabled: false,
      });

      let sqlQuery = (genaiResponse.content || '').trim();
      
      // Clean SQL: remove markdown code blocks and trailing backticks
      sqlQuery = sqlQuery.replace(/^```sql\s*/i, '').replace(/^```\s*/, '').replace(/\s*```\s*$/, '');
      sqlQuery = sqlQuery.replace(/`+$/, ''); // Remove trailing backticks
      sqlQuery = sqlQuery.trim();

      // Generate explanation
      let explanation = '';
      try {
        const explanationResponse = await this.genaiGenerate({
          model: 'gemini_fast',
          prompt: `Explain what this BigQuery SQL query does in simple terms:\n\n${sqlQuery}\n\nProvide a brief explanation (1-2 sentences).`,
          temperature: 0.3,
          maxOutputTokens: 256,
          webSearchEnabled: false,
          toolCallingEnabled: false,
          strictJSONEnabled: false,
        });
        explanation = (explanationResponse.content || '').trim();
      } catch (explanationError) {
        console.warn(`[API] Failed to generate explanation:`, explanationError);
      }

      console.log(`[API] generateSQL completed: sqlLength=${sqlQuery.length}, hasExplanation=${!!explanation}`);
      
      return {
        sql: sqlQuery,
        explanation: explanation || undefined,
      };
    } catch (error: any) {
      console.error(`[API] generateSQL error:`, error);
      throw error;
    }
  }

  async executeSQL(projectId: string, sql: string, maxResults?: number): Promise<{
    results: Array<Record<string, any>>;
    rowCount: number;
    error?: string;
  }> {
    console.log(`[API] executeSQL called: projectId=${projectId}, sqlLength=${sql.length}, maxResults=${maxResults}`);
    try {
      const res = await this.client.post(`/api/v1/projects/${projectId}/analytics/execute-sql`, {
        sql,
        maxResults,
      }, {
        timeout: 300000, // 5 minutes
      });
      console.log(`[API] executeSQL response:`, res.data);
      console.log(`[API] executeSQL response has error:`, !!res.data?.error);
      return res.data;
    } catch (error: any) {
      console.error(`[API] executeSQL error:`, error);
      return {
        results: [],
        rowCount: 0,
        error: error.response?.data?.error || error.response?.data?.message || error.message || 'Unknown error',
      };
    }
  }

  async queryBigQuery(projectId: string, query: string, maxResults?: number): Promise<{
    sql: string;
    results: Array<Record<string, any>>;
    rowCount: number;
    explanation?: string;
    error?: string;
    errorHistory?: string[];
  }> {
    console.log(`[API] queryBigQuery called: projectId=${projectId}, query=${query}, maxResults=${maxResults}`);
    try {
      // Set timeout to 5 minutes for BigQuery queries (they can take time)
      const res = await Promise.race([
        this.client.post(`/api/v1/projects/${projectId}/analytics/query`, {
          query,
          maxResults,
        }, {
          timeout: 300000, // 5 minutes
        }),
        new Promise((_, reject) => 
          setTimeout(() => reject(new Error('Request timeout after 5 minutes')), 300000)
        )
      ]) as any;
      
      console.log(`[API] queryBigQuery response:`, res.data);
      console.log(`[API] queryBigQuery response has error:`, !!res.data?.error);
      if (res.data?.errorHistory && res.data.errorHistory.length > 0) {
        console.log(`[API] queryBigQuery error history:`, res.data.errorHistory);
      }
      return res.data;
    } catch (error: any) {
      console.error(`[API] queryBigQuery error:`, error);
      console.error(`[API] queryBigQuery error details:`, {
        message: error.message,
        response: error.response?.data,
        status: error.response?.status,
        timeout: error.code === 'ECONNABORTED',
      });
      // Return error response structure
      return {
        sql: '',
        results: [],
        rowCount: 0,
        error: error.response?.data?.error || error.response?.data?.message || error.message || 'Unknown error',
      };
    }
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

  async docsExportToDocx(projectId: string, docId: string): Promise<Blob> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/docs/${docId}/export`, {}, { responseType: 'blob' });
    return res.data as Blob;
  }

  // Project Scenarios
  async scenariosList(projectId: string, parentId?: string | null): Promise<import('../types/projectScenario').ProjectScenario[]> {
    // If parentId is null or undefined, load all scenarios (backend will return all)
    // If parentId is specified, backend will filter by parentId
    const params = parentId !== null && parentId !== undefined ? `?parentId=${encodeURIComponent(parentId)}` : '';
    const res = await this.client.get(`/api/v1/projects/${projectId}/scenarios${params}`);
    return res.data as import('../types/projectScenario').ProjectScenario[];
  }

  async scenarioGet(projectId: string, scenarioId: string): Promise<import('../types/projectScenario').ProjectScenario> {
    const res = await this.client.get(`/api/v1/projects/${projectId}/scenarios/${scenarioId}`);
    return res.data as import('../types/projectScenario').ProjectScenario;
  }

  async scenarioCreate(projectId: string, data: import('../types/projectScenario').CreateProjectScenarioRequest): Promise<import('../types/projectScenario').ProjectScenario> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/scenarios`, data);
    return res.data as import('../types/projectScenario').ProjectScenario;
  }

  async scenarioUpdate(projectId: string, scenarioId: string, data: import('../types/projectScenario').UpdateProjectScenarioRequest): Promise<import('../types/projectScenario').ProjectScenario> {
    const res = await this.client.put(`/api/v1/projects/${projectId}/scenarios/${scenarioId}`, data);
    return res.data as import('../types/projectScenario').ProjectScenario;
  }

  async scenarioDelete(projectId: string, scenarioId: string): Promise<{ success: boolean; message: string }> {
    const res = await this.client.delete(`/api/v1/projects/${projectId}/scenarios/${scenarioId}`);
    return res.data as { success: boolean; message: string };
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

  // --- Agents ---
  /**
   * List all available agents
   */
  async listAgents(): Promise<import('../types/agents').AgentsListResponse> {
    const res = await this.client.get('/api/v1/agents');
    return res.data;
  }

  /**
   * Create Agent Job
   */
  async createAgentJob(projectId: string, request: {
    agentId: string;
    task: {
      userQuery: string;
      planMode?: string;
      [key: string]: any;
    };
    autoMode?: boolean;
    title?: string;
  }): Promise<{ job: any }> {
    const res = await this.client.post(`/api/v1/projects/${projectId}/agent-jobs`, request, {
      timeout: 30000, // 30 seconds for job creation
    });
    return res.data;
  }

  /**
   * Execute Agent Job
   */
  async executeAgentJob(projectId: string, jobId: string): Promise<void> {
    await this.client.post(`/api/v1/projects/${projectId}/agent-jobs/${jobId}/execute`, {}, {
      timeout: 10000, // 10 seconds for execution start (actual execution is async)
    });
  }

  /**
   * List Agent Jobs for a project
   */
  async listAgentJobs(projectId: string, options?: { status?: string; limit?: number }): Promise<{ jobs: any[]; count: number }> {
    const params: any = {};
    if (options?.status) params.status = options.status;
    if (options?.limit) params.limit = options.limit;
    const res = await this.client.get(`/api/v1/projects/${projectId}/agent-jobs`, { params });
    return res.data;
  }

  /**
   * Get Agent Job by ID
   * Returns full job details including steps, task, goal, etc.
   */
  async getAgentJob(projectId: string, jobId: string): Promise<{ job: any }> {
    const res = await this.client.get(`/api/v1/projects/${projectId}/agent-jobs/${jobId}`);
    return res.data;
  }

  /**
   * Update Agent Job (e.g., to update title)
   */
  async updateAgentJob(projectId: string, jobId: string, updates: { title?: string }): Promise<void> {
    await this.client.patch(`/api/v1/projects/${projectId}/agent-jobs/${jobId}`, updates);
  }

  /**
   * Pause Agent Job
   */
  async pauseAgentJob(projectId: string, jobId: string): Promise<void> {
    await this.client.post(`/api/v1/projects/${projectId}/agent-jobs/${jobId}/pause`);
  }

  /**
   * Resume Agent Job
   */
  async resumeAgentJob(projectId: string, jobId: string): Promise<void> {
    await this.client.post(`/api/v1/projects/${projectId}/agent-jobs/${jobId}/resume`);
  }

  /**
   * Cancel Agent Job (stop execution)
   */
  async cancelAgentJob(projectId: string, jobId: string): Promise<void> {
    await this.client.post(`/api/v1/projects/${projectId}/agent-jobs/${jobId}/cancel`);
  }

  /**
   * Delete Agent Job
   */
  async deleteAgentJob(projectId: string, jobId: string): Promise<void> {
    await this.client.delete(`/api/v1/projects/${projectId}/agent-jobs/${jobId}`);
  }

  /**
   * Execute Data Agent
   * @param projectId Project ID (required)
   * @param task Data Agent task
   */
  async executeDataAgent(
    projectId: string,
    task: import('../types/agents').DataAgentTask
  ): Promise<import('../types/agents').AgentExecutionResponse> {
    // Agents can take a long time (several minutes), so set a long timeout
    const res = await this.client.post(`/api/v1/projects/${projectId}/agents/data/execute`, task, {
      timeout: 600000, // 10 minutes
    });
    return res.data;
  }

  /**
   * Execute Entities Agent
   * @param projectId Project ID (required)
   * @param task Entities Agent task
   */
  async executeEntitiesAgent(
    projectId: string,
    task: import('../types/agents').EntitiesAgentTask
  ): Promise<import('../types/agents').AgentExecutionResponse> {
    // Agents can take a long time (several minutes), so set a long timeout
    const res = await this.client.post(`/api/v1/projects/${projectId}/agents/entities/execute`, task, {
      timeout: 600000, // 10 minutes
    });
    return res.data;
  }

  /**
   * Execute any agent by ID (generic method)
   * @param projectId Project ID (required)
   * @param agentId Agent ID (e.g., 'data', 'entities')
   * @param task Agent task (must match agent's TaskSchema)
   */
  async executeAgent(
    projectId: string,
    agentId: string,
    task: import('../types/agents').AgentTask
  ): Promise<import('../types/agents').AgentExecutionResponse> {
    // Agents can take a long time (several minutes), so set a long timeout
    const res = await this.client.post(`/api/v1/projects/${projectId}/agents/${agentId}/execute`, task, {
      timeout: 600000, // 10 minutes
    });
    return res.data;
  }

  // Usage metrics
  async getUsage(params?: { startDate?: string; endDate?: string; granularity?: '1d' | '7d' | '30d' }): Promise<{
    byModel: Record<string, {
      count: number;
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
    }>;
    dailyTotals: Record<string, {
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      count: number;
    }>;
    tokenCategories?: {
      premiumTokens: {
        inputTokens: number;
        outputTokens: number;
        totalTokens: number;
        costUSD: number;
        count: number;
      };
      regularTokens: {
        inputTokens: number;
        outputTokens: number;
        totalTokens: number;
        costUSD: number;
        count: number;
      };
    };
    totals: {
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      count: number;
    };
    period: {
      startDate: string;
      endDate: string;
    };
  }> {
    const res = await this.client.get(`/api/v1/usage`, { params });
    return res.data;
  }

  // Get paginated usage events
  async getUsageEvents(params: {
    startDate: string;
    endDate: string;
    page?: number;
    limit?: number;
  }): Promise<{
    events: Array<{
      id: string;
      tenantId: string;
      userId: string;
      projectId?: string;
      interactionId?: string;
      provider: string;
      model: string;
      modelAlias?: string;
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      taskType?: string;
      useCase?: string; // Business purpose: "chat", "bias_analysis", "frame_decomposition", etc.
      kind: string;
      createdAt: string;
    }>;
    pagination: {
      page: number;
      limit: number;
      total: number;
      totalPages: number;
    };
    period: {
      startDate: string;
      endDate: string;
    };
  }> {
    const res = await this.client.get(`/api/v1/usage/events`, { params });
    return res.data;
  }

  // Get average timing estimate for a model (without token count)
  async getAverageTiming(params: { provider: string; model: string }): Promise<{
    estimatedSeconds: number | null;
    provider: string;
    model: string;
  }> {
    const res = await this.client.get(`/api/v1/usage/avg-timing`, {
      params: {
        provider: params.provider,
        model: params.model,
      },
    });
    return res.data;
  }

  // Vouchers (admin only)
  async listVouchers(params?: { tier?: 'pro' | 'max'; isActive?: boolean }): Promise<Array<{
    id: string;
    code: string;
    tier: 'pro' | 'max';
    validFrom: string;
    validTo?: string;
    durationMonths: number;
    maxUses?: number;
    usedCount: number;
    isActive: boolean;
    createdBy: string;
    note?: string;
    createdAt: string;
    updatedAt: string;
  }>> {
    const res = await this.client.get(`/api/v1/vouchers`, { params });
    return res.data;
  }

  async getVoucher(voucherId: string): Promise<{
    id: string;
    code: string;
    tier: 'pro' | 'max';
    validFrom: string;
    validTo?: string;
    durationMonths: number;
    maxUses?: number;
    usedCount: number;
    isActive: boolean;
    createdBy: string;
    note?: string;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.get(`/api/v1/vouchers/${voucherId}`);
    return res.data;
  }

  async createVoucher(data: {
    code: string;
    tier: 'pro' | 'max';
    validFrom: string;
    validTo?: string;
    durationMonths: number;
    maxUses?: number;
    isActive: boolean;
    note?: string;
  }): Promise<{
    id: string;
    code: string;
    tier: 'pro' | 'max';
    validFrom: string;
    validTo?: string;
    durationMonths: number;
    maxUses?: number;
    usedCount: number;
    isActive: boolean;
    createdBy: string;
    note?: string;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.post(`/api/v1/vouchers`, data);
    return res.data;
  }

  async updateVoucher(voucherId: string, data: {
    tier?: 'pro' | 'max';
    validFrom?: string;
    validTo?: string;
    durationMonths?: number;
    maxUses?: number;
    isActive?: boolean;
    note?: string;
  }): Promise<{
    id: string;
    code: string;
    tier: 'pro' | 'max';
    validFrom: string;
    validTo?: string;
    durationMonths: number;
    maxUses?: number;
    usedCount: number;
    isActive: boolean;
    createdBy: string;
    note?: string;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.patch(`/api/v1/vouchers/${voucherId}`, data);
    return res.data;
  }

  async deleteVoucher(voucherId: string): Promise<{ message: string }> {
    const res = await this.client.delete(`/api/v1/vouchers/${voucherId}`);
    return res.data;
  }

  // Subscriptions
  async getSubscription(): Promise<{
    id: string;
    tenantId: string;
    userId: string;
    tier: 'free' | 'pro' | 'max';
    status: 'active' | 'expired' | 'cancelled';
    startedAt: string;
    currentPeriodStart: string;
    currentPeriodEnd: string;
    nextBillingDate?: string;
    cancelAtPeriodEnd?: boolean;
    voucherId?: string;
    paymentProvider?: string;
    paymentId?: string;
    paypalPlanId?: string;
    paypalAgreementId?: string;
    paypalSubscriptionId?: string;
    metadata?: Record<string, any>;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.get(`/api/v1/subscriptions/me`);
    return res.data;
  }

  async applyVoucher(code: string): Promise<{
    id: string;
    tenantId: string;
    userId: string;
    tier: 'free' | 'pro' | 'max';
    status: 'active' | 'expired' | 'cancelled';
    startedAt: string;
    currentPeriodStart: string;
    currentPeriodEnd: string;
    nextBillingDate?: string;
    voucherId?: string;
    paymentProvider?: string;
    paymentId?: string;
    paypalPlanId?: string;
    paypalAgreementId?: string;
    paypalSubscriptionId?: string;
    metadata?: Record<string, any>;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.post(`/api/v1/subscriptions/voucher`, { code });
    return res.data;
  }

  // PayPal
  async createPayPalSubscription(tier: 'pro' | 'max'): Promise<{ approvalUrl: string; subscriptionId: string }> {
    const res = await this.client.post(`/api/v1/paypal/subscription`, { tier });
    return res.data;
  }

  async executePayPalSubscription(subscriptionId: string): Promise<{
    id: string;
    tenantId: string;
    userId: string;
    tier: 'free' | 'pro' | 'max';
    status: 'active' | 'expired' | 'cancelled';
    startedAt: string;
    currentPeriodStart: string;
    currentPeriodEnd: string;
    nextBillingDate?: string;
    cancelAtPeriodEnd?: boolean;
    voucherId?: string;
    paymentProvider?: string;
    paymentId?: string;
    paypalPlanId?: string;
    paypalAgreementId?: string;
    paypalSubscriptionId?: string;
    metadata?: Record<string, any>;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.post(`/api/v1/paypal/subscription/execute`, { subscriptionId });
    return res.data;
  }

  async cancelPayPalSubscription(): Promise<{
    id: string;
    tenantId: string;
    userId: string;
    tier: 'free' | 'pro' | 'max';
    status: 'active' | 'expired' | 'cancelled';
    startedAt: string;
    currentPeriodStart: string;
    currentPeriodEnd: string;
    nextBillingDate?: string;
    cancelAtPeriodEnd: boolean;
    voucherId?: string;
    paymentProvider?: string;
    paymentId?: string;
    paypalPlanId?: string;
    paypalAgreementId?: string;
    paypalSubscriptionId?: string;
    metadata?: Record<string, any>;
    createdAt: string;
    updatedAt: string;
  }> {
    const res = await this.client.post(`/api/v1/paypal/subscription/cancel`);
    return res.data;
  }

  async getScenarioData(projectId: string, scenarioId: string): Promise<Array<import('../types/scenarioData').ScenarioData>> {
    console.log(`[API] getScenarioData called: projectId=${projectId}, scenarioId=${scenarioId}`);
    try {
      const res = await this.client.get(`/api/v1/projects/${projectId}/scenarios/${scenarioId}/data`);
      console.log(`[API] getScenarioData response:`, res.data);
      return res.data || [];
    } catch (error: any) {
      console.error(`[API] getScenarioData error:`, error);
      throw error;
    }
  }

  async createScenarioData(projectId: string, scenarioId: string, data: import('../types/scenarioData').ScenarioData): Promise<import('../types/scenarioData').ScenarioData> {
    console.log(`[API] createScenarioData called: projectId=${projectId}, scenarioId=${scenarioId}`);
    try {
      const res = await this.client.post(`/api/v1/projects/${projectId}/scenarios/${scenarioId}/data`, data);
      console.log(`[API] createScenarioData response:`, res.data);
      return res.data;
    } catch (error: any) {
      console.error(`[API] createScenarioData error:`, error);
      throw error;
    }
  }

  async deleteScenarioData(projectId: string, scenarioId: string, dataId: string): Promise<{ success: boolean; message: string }> {
    console.log(`[API] deleteScenarioData called: projectId=${projectId}, scenarioId=${scenarioId}, dataId=${dataId}`);
    try {
      const res = await this.client.delete(`/api/v1/projects/${projectId}/scenarios/${scenarioId}/data/${dataId}`);
      console.log(`[API] deleteScenarioData response:`, res.data);
      return res.data;
    } catch (error: any) {
      console.error(`[API] deleteScenarioData error:`, error);
      throw error;
    }
  }

  // Horizon - Entity Search
  async searchHorizonEntities(query: string, limit: number = 10, signal?: AbortSignal): Promise<{
    entities: Array<{
      id: string;
      canonicalPath: string;
      name: string;
      type: string;
      entityPath?: string;
      sectorPath?: string;
      description?: string;
      location?: string;
      sector?: string;
    }>;
    total: number;
    query: string;
  }> {
    try {
      const response = await this.client.post('/api/horizon/entities/search', {
        query,
        limit,
      }, {
        signal,
      });
      return response.data;
    } catch (error: any) {
      if (error.name === 'AbortError' || error.code === 'ERR_CANCELED') {
        throw error; // Re-throw abort errors as-is
      }
      console.error(`[API] searchHorizonEntities error:`, error);
      throw error;
    }
  }

  // Horizon - Lenses
  async createHorizonLens(input: {
    projectId: string;
    name: string;
    scope: string[];
    viewConfig?: Record<string, any>;
    alertConfig?: Record<string, any>;
    status?: string;
  }, options?: { correlationId?: string; sessionId?: string; skipScouting?: boolean }): Promise<{
    id: string;
    tenantId: string;
    projectId: string;
    name: string;
    scope: string[];
    viewConfig?: Record<string, any>;
    alertConfig?: Record<string, any>;
    status: string;
    createdAt: string;
    updatedAt: string;
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      if (options?.skipScouting) headers['X-Horizon-Skip-Scouting'] = 'true';
      const response = await this.client.post('/api/horizon/lenses', input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] createHorizonLens error:`, error);
      throw error;
    }
  }

  // Horizon - Intent precheck (NL -> taxonomy-aligned intent)
  async resolveHorizonIntent(input: { query: string }): Promise<{
    intent: {
      focus: string;
      spherePaths: string[];
      keywords: string[];
      anchors: Array<{ canonicalPath: string; type?: string; name?: string; confidence?: number; reason?: string }>;
    };
  }> {
    try {
      const response = await this.client.post('/api/horizon/intent/resolve', input);
      return response.data;
    } catch (error: any) {
      console.error(`[API] resolveHorizonIntent error:`, error);
      throw error;
    }
  }

  // Horizon - Lens preflight (resolve intent + recommend just_open vs research_first; no writes)
  async preflightHorizonLensFromQuery(
    input: {
      projectId: string;
      query: string;
      specialties?: string[];
    },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    intent: {
      focus: string;
      spherePaths: string[];
      keywords: string[];
      domains?: Array<{
        canonicalPath: string;
        contractType?: string;
        title?: string;
        description?: string;
        facets?: Record<string, string[]>;
        projections?: string[];
        relatedDomains?: string[];
        affectedDomains?: string[];
        firstOrderEntities?: string[];
        confidence?: number;
        reason?: string;
      }>;
      anchors: Array<{ canonicalPath: string; type?: string; name?: string; confidence?: number; reason?: string }>;
      decompositionHints?: string[];
    };
    recommendation: {
      mode: 'just_open' | 'research_first' | string;
      reason: string;
      stats: {
        anchorsTotal: number;
        anchorsKnown: number;
        anchorsFresh: number;
        domainsTotal: number;
        domainsKnown: number;
        hasDomains: boolean;
      };
    };
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post('/api/horizon/lenses/preflight', input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] preflightHorizonLensFromQuery error:`, error);
      throw error;
    }
  }

  // Horizon - Create Lens from NL query (resolve intent -> create lens)
  async createHorizonLensFromQuery(
    input: {
      projectId: string;
      query: string;
      name?: string;
      mode?: 'just_open' | 'research_first' | string;
      intent?: any; // Optional: edited intent override from preflight UI
      maxRecursionDepth?: number;
      requestedMetrics?: string[];
      prefetchScope?: string[];
      specialties?: string[];
      status?: string;
    },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    intent: {
      focus: string;
      spherePaths: string[];
      keywords: string[];
      domains?: Array<{
        canonicalPath: string;
        contractType?: string;
        title?: string;
        description?: string;
        facets?: Record<string, string[]>;
        projections?: string[];
        relatedDomains?: string[];
        affectedDomains?: string[];
        firstOrderEntities?: string[];
        confidence?: number;
        reason?: string;
      }>;
      anchors: Array<{ canonicalPath: string; type?: string; name?: string; confidence?: number; reason?: string }>;
      decompositionHints?: string[];
    };
    lens: {
      id: string;
      tenantId: string;
      projectId: string;
      name: string;
      scope: string[];
      viewConfig?: Record<string, any>;
      alertConfig?: Record<string, any>;
      status: string;
      createdAt: string;
      updatedAt: string;
    };
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post('/api/horizon/lenses/from-query', input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] createHorizonLensFromQuery error:`, error);
      throw error;
    }
  }

  async listHorizonLenses(projectId: string, options?: { status?: string; limit?: number }): Promise<{
    lenses: Array<{
      id: string;
      tenantId: string;
      projectId: string;
      name: string;
      scope: string[];
      viewConfig?: Record<string, any>;
      alertConfig?: Record<string, any>;
      status: string;
      createdAt: string;
      updatedAt: string;
    }>;
    total: number;
  }> {
    try {
      const params: Record<string, string> = { projectId };
      if (options?.status) {
        params.status = options.status;
      }
      if (options?.limit) {
        params.limit = options.limit.toString();
      }
      const response = await this.client.get('/api/horizon/lenses', { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonLenses error:`, error);
      throw error;
    }
  }

  async getHorizonLens(lensId: string): Promise<{
    id: string;
    tenantId: string;
    projectId: string;
    name: string;
    scope: string[];
    viewConfig?: Record<string, any>;
    alertConfig?: Record<string, any>;
    status: string;
    createdAt: string;
    updatedAt: string;
    scopeFragments?: Record<string, string>;
    resolvedQueries?: Record<string, string[]>;
    lastResolved?: string;
  }> {
    try {
      const response = await this.client.get(`/api/horizon/lenses/${lensId}`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonLens error:`, error);
      throw error;
    }
  }

  async updateHorizonLens(
    lensId: string,
    data: {
      id?: string;
      tenantId: string;
      projectId: string;
      name: string;
      scope: string[];
      viewConfig?: Record<string, any>;
      alertConfig?: Record<string, any>;
      status: string;
      scopeFragments?: Record<string, string>;
      resolvedQueries?: Record<string, string[]>;
      lastResolved?: string;
    },
    options?: { skipScouting?: boolean; correlationId?: string; sessionId?: string }
  ): Promise<{
    id: string;
    tenantId: string;
    projectId: string;
    name: string;
    scope: string[];
    viewConfig?: Record<string, any>;
    alertConfig?: Record<string, any>;
    status: string;
    createdAt: string;
    updatedAt: string;
    scopeFragments?: Record<string, string>;
    resolvedQueries?: Record<string, string[]>;
    lastResolved?: string;
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      if (options?.skipScouting) headers['X-Horizon-Skip-Scouting'] = '1';
      const response = await this.client.put(`/api/horizon/lenses/${lensId}`, data, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] updateHorizonLens error:`, error);
      throw error;
    }
  }

  async deleteHorizonLens(lensId: string): Promise<{ success: boolean }> {
    try {
      await this.client.delete(`/api/horizon/lenses/${lensId}`);
      return { success: true };
    } catch (error: any) {
      console.error(`[API] deleteHorizonLens error:`, error);
      throw error;
    }
  }

  /**
   * Transfer a lens from one project to another
   * POST /api/horizon/lenses/{lensId}/transfer
   */
  async transferHorizonLens(
    lensId: string,
    targetProjectId: string
  ): Promise<{
    id: string;
    tenantId: string;
    projectId: string;
    name: string;
    scope: string[];
    status: string;
    createdAt: string;
    updatedAt: string;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/transfer`, { targetProjectId });
      return response.data;
    } catch (error: any) {
      console.error(`[API] transferHorizonLens error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Add node to lens scope + run additional scouting (depth=1 by default)
   * POST /api/horizon/lenses/{lensId}/scope-scout
   */
  async scopeScoutHorizonLensNode(
    lensId: string,
    input: { fragmentId?: string; canonicalPath?: string; maxRecursionDepth?: number },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    jobId: string;
    scopeAdded: boolean;
    targetCanonicalPath: string;
    lens?: {
      id: string;
      tenantId: string;
      projectId: string;
      name: string;
      scope: string[];
      viewConfig?: Record<string, any>;
      alertConfig?: Record<string, any>;
      status: string;
      createdAt: string;
      updatedAt: string;
      scopeFragments?: Record<string, string>;
      resolvedQueries?: Record<string, string[]>;
      lastResolved?: string;
    };
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/scope-scout`, input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] scopeScoutHorizonLensNode error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Resolve query -> add anchor node to lens scope + run scouting (single-node by default)
   * POST /api/horizon/lenses/{lensId}/scope-scout-query
   */
  async scopeScoutHorizonLensQuery(
    lensId: string,
    input: { query: string; specialties?: string[] },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    jobId: string;
    scopeAdded: boolean;
    targetCanonicalPath: string;
    lens?: {
      id: string;
      tenantId: string;
      projectId: string;
      name: string;
      scope: string[];
      viewConfig?: Record<string, any>;
      alertConfig?: Record<string, any>;
      status: string;
      createdAt: string;
      updatedAt: string;
      scopeFragments?: Record<string, string>;
      resolvedQueries?: Record<string, string[]>;
      lastResolved?: string;
    };
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/scope-scout-query`, input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] scopeScoutHorizonLensQuery error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Ask a grounded question against the currently visible graph snapshot.
   * POST /api/horizon/lenses/{lensId}/graph-query
   */
  async queryHorizonGraph(
    lensId: string,
    input: {
      projectId?: string;
      question: string;
      domainAnswerContext?: {
        snapshotId?: string;
        id?: string;
        domainId?: string;
        domainCanonicalPath?: string;
        readinessStatus?: string;
        domainContract?: {
          contractType?: string;
          title?: string;
          description?: string;
          facets?: Record<string, string[]>;
        };
        thesis?: { text?: string; confidence?: number };
        gate?: { overallStatus?: string; failedCheckIds?: string[] };
        criticalDependencies?: Array<{
          sourceCanonicalPath?: string;
          targetCanonicalPath?: string;
          relationType?: string;
          topDependencyScore?: number;
          confidence?: number;
        }>;
        impactPaths?: Array<{
          nodePaths?: string[];
          edgeIds?: string[];
          criticality?: number;
          facetKey?: string;
        }>;
        dataGaps?: Array<{
          gapType?: string;
          affectedFacet?: string;
          affectedNodePath?: string;
          affectedEdgeId?: string;
          hint?: string;
          priority?: string;
        }>;
      };
      graph: {
        nodes: Array<{ id: string; label?: string; type?: string; canonicalPath?: string }>;
        edges?: Array<{ from: string; to: string; type?: string; label?: string }>;
      };
    }
  ): Promise<{
    interactionId: string;
    projectId: string;
    lensId: string;
    question: string;
    answer: string;
    relevantNodeIds: string[];
    findings?: Array<{ nodeId: string; title: string; eventDate: string; source: string; url: string }>;
    model?: string;
    searchQueries?: string[];
    sources?: string[];
    timestamp: string;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/graph-query`, input);
      return response.data;
    } catch (error: any) {
      console.error(`[API] queryHorizonGraph error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Build a structured graph-intent plan from NL query + visible graph snapshot.
   * POST /api/horizon/lenses/{lensId}/graph-intent/plan
   */
  async planHorizonGraphIntent(
    lensId: string,
    input: import('../types/graphIntent').GraphIntentPlanRequest,
  ): Promise<import('../types/graphIntent').GraphIntentPlanResponse> {
    try {
      const response = await this.client.post<import('../types/graphIntent').GraphIntentPlanResponse>(
        `/api/horizon/lenses/${lensId}/graph-intent/plan`,
        input,
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] planHorizonGraphIntent error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Apply a graph-intent plan (mutating mode).
   * POST /api/horizon/lenses/{lensId}/graph-intent/apply
   */
  async applyHorizonGraphIntent(
    lensId: string,
    input: import('../types/graphIntent').GraphIntentApplyRequest,
    options?: { confirm?: boolean },
  ): Promise<import('../types/graphIntent').GraphIntentApplyResponse> {
    try {
      const response = await this.client.post<import('../types/graphIntent').GraphIntentApplyResponse>(
        `/api/horizon/lenses/${lensId}/graph-intent/apply`,
        input,
        {
          params: options?.confirm ? { confirm: 'true' } : undefined,
          headers: options?.confirm ? { 'X-Graph-Intent-Confirm': 'true' } : undefined,
        },
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] applyHorizonGraphIntent error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Execute graph-intent in dry-run mode (no writes).
   * POST /api/horizon/lenses/{lensId}/graph-intent/dry-run
   */
  async dryRunHorizonGraphIntent(
    lensId: string,
    input: import('../types/graphIntent').GraphIntentApplyRequest,
  ): Promise<import('../types/graphIntent').GraphIntentApplyResponse> {
    try {
      const response = await this.client.post<import('../types/graphIntent').GraphIntentApplyResponse>(
        `/api/horizon/lenses/${lensId}/graph-intent/dry-run`,
        input,
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] dryRunHorizonGraphIntent error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Build transitive control corridor (authority -> decision -> mechanism -> impact)
   * on the fly from graph data around seeds/lens scope.
   * POST /api/horizon/lenses/{lensId}/control-corridor
   */
  async queryHorizonControlCorridor(
    lensId: string,
    input?: {
      seedCanonicalPaths?: string[];
      seedFragmentIds?: string[];
      maxAuthorityHops?: number;
      maxImpactHops?: number;
      perNodeEdgeLimit?: number;
      maxNodes?: number;
      maxEdges?: number;
      maxSeeds?: number;
    }
  ): Promise<{
    lensId: string;
    seedFragmentIds: string[];
    seedCanonicalPaths?: string[];
    nodes: Array<{
      id: string;
      label: string;
      type?: string;
      canonicalPath?: string;
      lane: 'authority' | 'decision' | 'mechanism' | 'impact' | 'intermediary' | string;
      depth: number;
      seed?: boolean;
    }>;
    edges: Array<{
      id: string;
      from: string;
      to: string;
      type: string;
      stage: 'authority' | 'decision' | 'mechanism' | 'impact' | string;
      role?: 'authority' | 'execution' | 'propagation' | string;
      criticality?: number;
    }>;
    bundles?: {
      controllers?: Array<{
        id: string;
        label: string;
        type?: string;
        canonicalPath?: string;
        score?: number;
        coverage?: number;
      }>;
      decisionPoints?: Array<{
        id: string;
        label: string;
        type?: string;
        canonicalPath?: string;
        score?: number;
        coverage?: number;
      }>;
      mechanisms?: Array<{
        id: string;
        label: string;
        type?: string;
        canonicalPath?: string;
        score?: number;
        coverage?: number;
      }>;
      mediators?: Array<{
        id: string;
        label: string;
        type?: string;
        canonicalPath?: string;
        score?: number;
        coverage?: number;
      }>;
      impactedTargets?: Array<{
        id: string;
        label: string;
        type?: string;
        canonicalPath?: string;
        score?: number;
        coverage?: number;
      }>;
      paths?: Array<{
        id: string;
        controllerId: string;
        targetId: string;
        mediatorId?: string;
        nodeIds: string[];
        edgeIds: string[];
        edgeKeys?: string[];
        coverageScore?: number;
        description?: string;
      }>;
      bottlenecks?: {
        nodes?: Array<{
          id: string;
          label?: string;
          type?: string;
          canonicalPath?: string;
          usageCount?: number;
          score?: number;
        }>;
        edges?: Array<{
          id: string;
          from: string;
          to: string;
          type?: string;
          role?: string;
          usageCount?: number;
        }>;
      };
    };
    summary: {
      seedCount: number;
      seedTruncated?: boolean;
      authorityCount: number;
      decisionCount: number;
      mechanismCount: number;
      impactCount: number;
      impactRootCount: number;
      controllerCount?: number;
      mediatorsCount?: number;
      pathBundleCount?: number;
      totalNodes: number;
      totalEdges: number;
      maxAuthorityHops: number;
      maxImpactHops: number;
      truncated: boolean;
    };
    warnings?: string[];
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/control-corridor`, input || {});
      return response.data;
    } catch (error: any) {
      console.error(`[API] queryHorizonControlCorridor error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Preview strategic domain overlay (no job creation).
   * POST /api/horizon/lenses/{lensId}/domain-overlay-preview
   */
  async previewDomainOverlay(
    lensId: string,
    input: { query: string }
  ): Promise<{
    intent: any;
    existingDomains?: string[];
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/domain-overlay-preview`, input);
      return response.data;
    } catch (error: any) {
      console.error(`[API] previewDomainOverlay error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Execute strategic domain overlay job.
   * POST /api/horizon/lenses/{lensId}/domain-overlay-execute
   */
  async executeDomainOverlay(
    lensId: string,
    input: { query: string; domainCanonicalPath?: string; specialties?: string[] },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    jobId: string;
    domainCanonicalPath: string;
    existingDomains?: string[];
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/domain-overlay-execute`, input, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] executeDomainOverlay error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - List domain answers for a lens.
   * GET /api/horizon/lenses/{lensId}/domain-answers
   */
  async listHorizonDomainAnswers(
    lensId: string,
    options?: { limit?: number }
  ): Promise<{ domainAnswers: any[]; totalCount: number }> {
    try {
      const params: Record<string, number> = {};
      if (typeof options?.limit === 'number' && options.limit > 0) {
        params.limit = options.limit;
      }
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/domain-answers`, { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonDomainAnswers error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Get one domain answer by domainId or snapshot id.
   * GET /api/horizon/lenses/{lensId}/domain-answers/{domainId}
   */
  async getHorizonDomainAnswer(lensId: string, domainId: string): Promise<any> {
    try {
      const response = await this.client.get(
        `/api/horizon/lenses/${lensId}/domain-answers/${encodeURIComponent(domainId)}`
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonDomainAnswer error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Create a new domain on demand from a user query.
   * POST /api/horizon/lenses/{lensId}/domain-answers
   */
  async createHorizonDomainAnswer(
    lensId: string,
    body: { query: string },
    headers?: { correlationId?: string; sessionId?: string },
  ): Promise<{ jobId: string; domainCanonicalPath: string }> {
    try {
      const hdrs: Record<string, string> = {};
      if (headers?.correlationId) hdrs['X-Correlation-Id'] = headers.correlationId;
      if (headers?.sessionId) hdrs['X-Session-Id'] = headers.sessionId;
      const response = await this.client.post(
        `/api/horizon/lenses/${lensId}/domain-answers`,
        body,
        { headers: hdrs },
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] createHorizonDomainAnswer error:`, error);
      throw error;
    }
  }

  /**
   * Horizon - Run DomainContractEngine for an existing domain (builds snapshot).
   * POST /api/horizon/lenses/{lensId}/domain-answers/{domainId}/run
   */
  async runHorizonDomainAnswer(
    lensId: string,
    domainId: string,
    headers?: { correlationId?: string; sessionId?: string },
  ): Promise<{ jobId: string; domainCanonicalPath: string }> {
    try {
      const hdrs: Record<string, string> = {};
      if (headers?.correlationId) hdrs['X-Correlation-Id'] = headers.correlationId;
      if (headers?.sessionId) hdrs['X-Session-Id'] = headers.sessionId;
      const response = await this.client.post(
        `/api/horizon/lenses/${lensId}/domain-answers/${encodeURIComponent(domainId)}/run`,
        {},
        { headers: hdrs },
      );
      return response.data;
    } catch (error: any) {
      console.error(`[API] runHorizonDomainAnswer error:`, error);
      throw error;
    }
  }

  async getHorizonLensAssemblyStatus(lensId: string): Promise<{
    status: string;
    phase: string;
    progress: number;
    phases: {
      decomposition: { status: string; nodes: number };
      cacheHit: { status: string; nodes: number };
      scouting: { status: string; completed: number; total: number; logs: string[] };
      ready: { status: string };
    };
    graph: {
      nodes: any[];
      edges: any[];
    };
  }> {
    try {
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/assembly-status`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonLensAssemblyStatus error:`, error);
      throw error;
    }
  }

  async listHorizonScoutingJobs(lensId: string, options?: { status?: string; limit?: number }): Promise<{
    jobs: Array<{
      id: string;
      status: string;
      targetCanonicalPath: string;
      createdAt: string;
      completedAt?: string | null;
    }>;
    total: number;
  }> {
    try {
      const params: Record<string, string> = { lensId };
      if (options?.status) {
        params.status = options.status;
      }
      if (options?.limit) {
        params.limit = options.limit.toString();
      }
      const response = await this.client.get('/api/horizon/scout/jobs', { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonScoutingJobs error:`, error);
      throw error;
    }
  }

  // --- Horizon LensItems ---
  async getLensItems(lensId: string): Promise<import('../types/lensItem').LensItem[]> {
    try {
      const response = await this.client.get<{ items: import('../types/lensItem').LensItem[] }>(`/api/horizon/lenses/${lensId}/items`);
      return response.data.items;
    } catch (error: any) {
      console.error(`[API] getLensItems error:`, error);
      throw error;
    }
  }

  async createLensItem(lensId: string, data: Partial<import('../types/lensItem').LensItem>): Promise<{ id: string }> {
    try {
      const response = await this.client.post<{ id: string }>(`/api/horizon/lenses/${lensId}/items`, data);
      return response.data;
    } catch (error: any) {
      console.error(`[API] createLensItem error:`, error);
      throw error;
    }
  }

  async updateLensItem(lensId: string, itemId: string, data: Partial<import('../types/lensItem').LensItem>): Promise<{ success: boolean }> {
    try {
      const response = await this.client.patch<{ success: boolean }>(`/api/horizon/lenses/${lensId}/items/${itemId}`, data);
      return response.data;
    } catch (error: any) {
      console.error(`[API] updateLensItem error:`, error);
      throw error;
    }
  }

  async deleteLensItem(lensId: string, itemId: string): Promise<{ success: boolean }> {
    try {
      const response = await this.client.delete<{ success: boolean }>(`/api/horizon/lenses/${lensId}/items/${itemId}`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] deleteLensItem error:`, error);
      throw error;
    }
  }

  // --- Horizon Fragments ---
  async getHorizonFragment(fragmentId: string, signal?: AbortSignal): Promise<any> {
    try {
      const response = await this.client.get(`/api/horizon/fragments/${fragmentId}`, { signal });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonFragment error:`, error);
      throw error;
    }
  }

  async getHorizonFragmentByPath(canonicalPath: string): Promise<any> {
    try {
      const response = await this.client.get('/api/horizon/fragments/by-path', {
        params: { path: canonicalPath },
      });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonFragmentByPath error:`, error);
      throw error;
    }
  }

  async listHorizonFragments(lensId: string): Promise<{
    fragments: any[];
    total: number;
  }> {
    try {
      const response = await this.client.get('/api/horizon/fragments', {
        params: { lensId },
      });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonFragments error:`, error);
      throw error;
    }
  }

  async listHorizonGeoObjects(options?: {
    limit?: number;
    pathPrefix?: string;
    onlyLoc?: boolean;
  }): Promise<{
    items: Array<{
      id: string;
      canonicalPath: string;
      type: string;
      label?: string;
      description?: string;
      status?: string;
      coordinates?: { lat: number; lon: number };
      updatedAt?: string;
    }>;
    total: number;
    limit: number;
    pathPrefix?: string;
  }> {
    try {
      const params: Record<string, string> = {};
      if (options?.limit !== undefined) params.limit = String(options.limit);
      if (options?.pathPrefix) params.pathPrefix = options.pathPrefix;
      if (options?.onlyLoc !== undefined) params.onlyLoc = options.onlyLoc ? '1' : '0';
      const response = await this.client.get('/api/horizon/geo-objects', { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonGeoObjects error:`, error);
      throw error;
    }
  }

  async deleteHorizonFragment(fragmentId: string): Promise<{ ok: boolean; fragmentId: string }> {
    try {
      const response = await this.client.delete(`/api/horizon/fragments/${fragmentId}`);
      if (response?.data && typeof response.data === 'object') {
        return response.data as { ok: boolean; fragmentId: string };
      }
      return { ok: true, fragmentId };
    } catch (error: any) {
      console.error(`[API] deleteHorizonFragment error:`, error);
      throw error;
    }
  }

  // --- Horizon Product IO (Hypothesis workflow, v1 PRODUCT-only) ---
  async extractProductIO(
    fragmentId: string,
    body?: { direction?: 'produces' | 'consumes' | 'both'; hint?: string; lang?: string }
  ): Promise<{
    fragmentId: string;
    direction: string;
    produces: Array<{ productName: string; confidence: number; reason?: string }>;
    consumes: Array<{ productName: string; confidence: number; reason?: string }>;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/fragments/${fragmentId}/product-io:extract`, body || {});
      return response.data;
    } catch (error: any) {
      console.error(`[API] extractProductIO error:`, error);
      throw error;
    }
  }

  async applyProductIO(
    lensId: string,
    body: {
      objectiveFragmentId: string;
      produces: Array<{ productName: string; confidence: number; reason?: string }>;
      consumes: Array<{ productName: string; confidence: number; reason?: string }>;
    }
  ): Promise<{
    lensId: string;
    objectiveFragmentId: string;
    delta: { fragmentsUpserted: any[]; edgesUpserted: any[] };
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/product-io:apply`, body);
      return response.data;
    } catch (error: any) {
      console.error(`[API] applyProductIO error:`, error);
      throw error;
    }
  }

  // Ensure Entity for fragment (create/link/enrich)
  async ensureFragmentEntity(fragmentId: string): Promise<{
    entityId: string;
    entity: any;
    created: boolean;
    enriched: boolean;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/fragments/${fragmentId}/ensure-entity`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] ensureFragmentEntity error:`, error);
      throw error;
    }
  }

  // Get Entity linked to fragment
  async getFragmentEntity(fragmentId: string): Promise<any> {
    try {
      const response = await this.client.get(`/api/horizon/fragments/${fragmentId}/entity`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getFragmentEntity error:`, error);
      throw error;
    }
  }

  // Enrich Entity linked to fragment (force refresh if force=true)
  async enrichFragmentEntity(fragmentId: string, force: boolean = false): Promise<{
    entityId: string;
    entity: any;
    enriched: boolean;
  }> {
    try {
      const params = force ? { force: '1' } : {};
      const response = await this.client.post(`/api/horizon/fragments/${fragmentId}/enrich-entity`, null, { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] enrichFragmentEntity error:`, error);
      throw error;
    }
  }

  // --- Horizon Edges ---
  async getHorizonEdge(edgeId: string): Promise<any> {
    try {
      const response = await this.client.get(`/api/horizon/edges/${edgeId}`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonEdge error:`, error);
      throw error;
    }
  }

  // --- Horizon AI Explanations (cached markdown) ---
  async getHorizonAIExplanation(
    targetType: 'edge' | 'node',
    targetId: string,
    options?: { lang?: string }
  ): Promise<{
    targetType: 'edge' | 'node';
    targetId: string;
    language: string;
    markdown: string;
    updatedAt: string;
    docId: string;
  }> {
    const lang = options?.lang || 'en';
    const response = await this.client.get(`/api/horizon/ai-explanations/${targetType}/${encodeURIComponent(targetId)}`, {
      params: { lang },
    });
    return response.data;
  }

  async upsertHorizonAIExplanation(
    targetType: 'edge' | 'node',
    targetId: string,
    data: {
      markdown: string;
      language?: string;
      model?: string;
      lensId?: string;
      edgeType?: string;
      sourceFragmentId?: string;
      targetFragmentId?: string;
      attrs?: Record<string, any>;
    },
    options?: { lang?: string }
  ): Promise<{ ok: boolean; targetType: string; targetId: string; language: string; docId: string }> {
    const lang = options?.lang || data.language || 'en';
    const response = await this.client.put(`/api/horizon/ai-explanations/${targetType}/${encodeURIComponent(targetId)}`, data, {
      params: { lang },
    });
    return response.data;
  }

  async createHorizonEdge(edge: {
    sourceFragmentId: string;
    targetFragmentId: string;
    type: string;
    properties?: {
      criticality?: number;
      amplification?: number;
      latencyHours?: number;
    };
    qualifiers?: Record<string, any>;
  }): Promise<any> {
    try {
      const response = await this.client.post('/api/horizon/edges', edge);
      return response.data;
    } catch (error: any) {
      console.error(`[API] createHorizonEdge error:`, error);
      throw error;
    }
  }

  async updateHorizonEdge(
    edgeId: string,
    updates: {
      properties?: {
        criticality?: number;
        amplification?: number;
        latencyHours?: number;
      };
      qualifiers?: Record<string, any>;
    }
  ): Promise<any> {
    try {
      const response = await this.client.put(`/api/horizon/edges/${edgeId}`, updates);
      return response.data;
    } catch (error: any) {
      console.error(`[API] updateHorizonEdge error:`, error);
      throw error;
    }
  }

  async deleteHorizonEdge(edgeId: string): Promise<void> {
    try {
      await this.client.delete(`/api/horizon/edges/${edgeId}`);
    } catch (error: any) {
      console.error(`[API] deleteHorizonEdge error:`, error);
      throw error;
    }
  }

  // --- Human-in-the-Loop (HITL) edge curation ---
  // Admin-only: POST /api/horizon/edges/{edgeId}/hitl
  async humanInTheLoopUpdateHorizonEdge(
    edgeId: string,
    data: { type?: string; swap?: boolean },
  ): Promise<any> {
    try {
      const response = await this.client.post(`/api/horizon/edges/${edgeId}/hitl`, data || {});
      return response.data;
    } catch (error: any) {
      console.error(`[API] humanInTheLoopUpdateHorizonEdge error:`, error);
      throw error;
    }
  }

  // --- Horizon Rollup & Metrics ---
  async getFragmentRollup(fragmentId: string, kind: string = 'risk'): Promise<{
    fragmentId: string;
    canonicalPath: string;
    metricKind: string;
    metricName?: string;
    value: number;
    confidence: number;
    metrics?: Record<string, number>;
    explainTrace?: {
      topContributors: Array<{
        signalId: string;
        signalType: string;
        sourcePath: string;
        contribution: number;
        reasoning: string;
      }>;
      propagationPath: string[];
      rulesApplied: string[];
    };
  }> {
    try {
      const response = await this.client.get(`/api/horizon/rollup/${fragmentId}`, {
        params: { kind },
      });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getFragmentRollup error:`, error);
      throw error;
    }
  }

  async getFragmentExplain(fragmentId: string, kind: string = 'risk'): Promise<{
    topContributors: Array<{
      signalId: string;
      signalType: string;
      sourcePath: string;
      contribution: number;
      reasoning: string;
    }>;
    propagationPath: string[];
    rulesApplied: string[];
  }> {
    try {
      const response = await this.client.get(`/api/horizon/rollup/${fragmentId}/explain`, {
        params: { kind },
      });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getFragmentExplain error:`, error);
      throw error;
    }
  }

  // --- Horizon Fragment Profiles ---
  async getFragmentProfile(fragmentId: string): Promise<any> {
    try {
      const response = await this.client.get(`/api/horizon/fragments/${fragmentId}/profile`);
      return response.data;
    } catch (error: any) {
      if (error?.response?.status === 404) {
        return null; // Profile not found
      }
      console.error(`[API] getFragmentProfile error:`, error);
      throw error;
    }
  }

  async deleteFragmentProfile(fragmentId: string): Promise<{ ok: boolean; fragmentId: string; deletedCount?: number }> {
    try {
      const response = await this.client.delete(`/api/horizon/fragments/${fragmentId}/profile`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] deleteFragmentProfile error:`, error);
      throw error;
    }
  }

  async refreshFragmentProfile(fragmentId: string, force: boolean = false): Promise<{
    profileId: string;
    fragmentId: string;
    created: boolean;
    durationMs: number;
    capturedAt: string;
    sectionsCount: number;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/fragments/${fragmentId}/profile/refresh`, { force });
      return response.data;
    } catch (error: any) {
      console.error(`[API] refreshFragmentProfile error:`, error);
      throw error;
    }
  }

  // --- Horizon Fragment Dependencies (direct edges / neighbors) ---
  async getFragmentDependencies(
    fragmentId: string,
    options?: {
      direction?: 'incoming' | 'outgoing' | 'both';
      // Prefer edgeTypes for multiple values (comma-separated string).
      edgeTypes?: string[];
      includeFragments?: boolean;
      includeUnindexed?: boolean;
      includeCounts?: boolean;
      // Optional budgets (applied only when provided)
      maxNeighbors?: number;
      maxEdges?: number;
      maxPerType?: number;
      limit?: number;
      // Optional: semantic=upstream|downstream|both (keeps backend default unless provided)
      semantic?: 'upstream' | 'downstream' | 'both';
      // Optional: lensId (for lens-aware filtering on backend). Usually omit for inspector.
      lensId?: string;
      // Optional request abort.
      signal?: AbortSignal;
    }
  ): Promise<{
    edges: any[];
    total: number;
    fragments?: any[];
    counts?: any;
  }> {
    try {
      const direction = options?.direction || 'both';
      const edgeTypes = Array.isArray(options?.edgeTypes) ? options!.edgeTypes : [];
      const params: any = {
        direction,
      };
      if (edgeTypes.length > 0) {
        // Backend supports edgeTypes=OWNS,CONTROLS,...
        params.edgeTypes = edgeTypes.join(',');
      }
      if (options?.includeFragments) params.includeFragments = '1';
      if (options?.includeUnindexed) params.includeUnindexed = '1';
      if (options?.includeCounts) params.includeCounts = '1';
      if (typeof options?.maxNeighbors === 'number' && options.maxNeighbors > 0) params.maxNeighbors = String(options.maxNeighbors);
      if (typeof options?.maxEdges === 'number' && options.maxEdges > 0) params.maxEdges = String(options.maxEdges);
      if (typeof options?.maxPerType === 'number' && options.maxPerType > 0) params.maxPerType = String(options.maxPerType);
      if (typeof options?.limit === 'number' && options.limit > 0) params.limit = String(options.limit);
      if (options?.semantic) params.semantic = options.semantic;

      const headers: any = {};
      if (options?.lensId) {
        // Backend reads lensId from query OR X-Lens-Id header; we pass query when possible, but keep header too.
        params.lensId = options.lensId;
        headers['X-Lens-Id'] = options.lensId;
      }

      const response = await this.client.get(`/api/horizon/fragments/${fragmentId}/dependencies`, {
        params,
        headers,
        signal: options?.signal,
      });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getFragmentDependencies error:`, error);
      throw error;
    }
  }

  async getHorizonLensCockpit(lensId: string): Promise<{
    lens: {
      id: string;
      name: string;
      scope: string[];
      metrics: Record<string, number>;
      viewConfig?: Record<string, any>;
    };
    graph: {
      nodes: any[];
      edges: any[];
    };
    domainAnswers?: any[];
    summary: {
      totalFragments: number;
      totalEdges: number;
      metrics: Record<string, number>;
    };
  }> {
    try {
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/cockpit`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonLensCockpit error:`, error);
      throw error;
    }
  }

  async recalculateHorizonLensMetrics(lensId: string): Promise<{
    jobId: string;
    status: string;
    estimatedCompletion: string | null;
    metrics: Record<string, number>;
  }> {
    try {
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/recalculate`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] recalculateHorizonLensMetrics error:`, error);
      throw error;
    }
  }

  // --- Horizon Lens Artifacts ---
  /**
   * List artifacts for a lens
   * GET /api/horizon/lenses/{lensId}/artifacts
   */
  async listHorizonLensArtifacts(
    lensId: string,
    options?: { type?: 'fragment' | 'edge' | 'FRAGMENT' | 'EDGE'; limit?: number }
  ): Promise<{
    artifacts: any[];
    totalCount: number;
    fragmentCount: number;
    edgeCount: number;
    unprocessedCount: number;
  }> {
    try {
      const params: Record<string, string> = {};
      if (options?.type) params.type = String(options.type).toUpperCase();
      if (options?.limit) params.limit = options.limit.toString();
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/artifacts`, { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] listHorizonLensArtifacts error:`, error);
      throw error;
    }
  }

  /**
   * Get artifact statistics for a lens
   * GET /api/horizon/lenses/{lensId}/artifacts/stats
   */
  async getHorizonLensArtifactStats(lensId: string): Promise<{
    totalArtifacts: number;
    fragmentArtifacts: number;
    edgeArtifacts: number;
    processedForPhase2: number;
    unprocessedForPhase2: number;
    lastUpdated?: string | null;
  }> {
    try {
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/artifacts/stats`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonLensArtifactStats error:`, error);
      throw error;
    }
  }

  /**
   * Get simplified context for LLM (connectivity-paginated)
   * GET /api/horizon/lenses/{lensId}/artifacts/context
   */
  async getHorizonLensArtifactContext(
    lensId: string,
    options?: { page?: number; pageSize?: number }
  ): Promise<{
    fragments: any[];
    edges: any[];
    pageNumber: number;
    totalPages: number;
    totalArtifacts: number;
    nextCursor?: string | null;
  }> {
    try {
      const params: Record<string, string> = {};
      if (options?.page !== undefined) params.page = options.page.toString();
      if (options?.pageSize !== undefined) params.pageSize = options.pageSize.toString();
      const response = await this.client.get(`/api/horizon/lenses/${lensId}/artifacts/context`, { params });
      return response.data;
    } catch (error: any) {
      console.error(`[API] getHorizonLensArtifactContext error:`, error);
      throw error;
    }
  }

  /**
   * Trigger strategic projection (decision/mechanism layer) for a lens
   * POST /api/horizon/lenses/{lensId}/trigger-phase2
   */
  async triggerHorizonPhase2StrategicOverlay(
    lensId: string,
    input?: { domainPaths?: string[]; force?: boolean },
    options?: { correlationId?: string; sessionId?: string }
  ): Promise<{
    jobIds: string[];
    message: string;
    queued: number;
  }> {
    try {
      const headers: Record<string, string> = {};
      if (options?.correlationId) headers['X-Correlation-Id'] = options.correlationId;
      if (options?.sessionId) headers['X-Session-Id'] = options.sessionId;
      const response = await this.client.post(`/api/horizon/lenses/${lensId}/trigger-phase2`, input || {}, { headers });
      return response.data;
    } catch (error: any) {
      console.error(`[API] triggerHorizonPhase2StrategicOverlay error:`, error);
      throw error;
    }
  }

  /**
   * Delete all artifacts for a lens
   * DELETE /api/horizon/lenses/{lensId}/artifacts
   */
  async deleteHorizonLensArtifacts(lensId: string): Promise<{ message: string; lensId: string }> {
    try {
      const response = await this.client.delete(`/api/horizon/lenses/${lensId}/artifacts`);
      return response.data;
    } catch (error: any) {
      console.error(`[API] deleteHorizonLensArtifacts error:`, error);
      throw error;
    }
  }
}

export const api = new ApiClient();
