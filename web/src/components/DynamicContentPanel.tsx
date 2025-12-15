import React from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { isArtifact, type Artifact } from '../types/artifact';
import type { CoreDoc } from '../types/generated';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getPreferredLanguage, getLanguageName } from '../lib/uiUtils';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getAverageTiming } from '../lib/timingCache';
import { mapAliasToModel } from '../lib/modelMapping';

interface ArtifactTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  type: 'system' | 'community' | 'custom';
  isFeatured?: boolean;
  defaultStructure?: Array<{
    id: string;
    title: string;
    order: number;
    description: string;
    promptTemplate: string;
    required: boolean;
    editable: boolean;
  }>;
  recommendedFormat?: string;
  recommendedFocus?: string;
}

interface DynamicContentPanelProps {
  projectId: string | null;
  onOpenDocument: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
  onCreateFromTemplate?: (templateId: string) => void;
}

export const DynamicContentPanel: React.FC<DynamicContentPanelProps> = ({
  projectId,
  onOpenDocument,
  onCreateFromTemplate,
}) => {
  const { t } = useTranslation();
  const [artifacts, setArtifacts] = React.useState<Artifact[]>([]);
  const [templates, setTemplates] = React.useState<ArtifactTemplate[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [editingArtifactId, setEditingArtifactId] = React.useState<string | null>(null);
  const [editingArtifactName, setEditingArtifactName] = React.useState<string>('');
  const editingArtifactInputRef = React.useRef<HTMLInputElement>(null);
  const [templatesLoading, setTemplatesLoading] = React.useState(false);
  const [showTemplates, setShowTemplates] = React.useState(false);
  const [selectedTemplate, setSelectedTemplate] = React.useState<ArtifactTemplate | null>(null);
  const [wizardStep, setWizardStep] = React.useState<'templates' | 'configure' | 'preview'>('templates');
  const [isMobile, setIsMobile] = React.useState(false);
  const [generatedSections, setGeneratedSections] = React.useState<Record<string, { content: string; error?: string }>>({});
  const [generating, setGenerating] = React.useState(false);
  const [generationProgress, setGenerationProgress] = React.useState<{ current: number; total: number }>({ current: 0, total: 0 });
  const [refreshingSectionId, setRefreshingSectionId] = React.useState<string | null>(null);

  // Create template wizard state
  const [showCreateTemplateWizard, setShowCreateTemplateWizard] = React.useState(false);
  const [templateUserRequest, setTemplateUserRequest] = React.useState('');
  const [isGeneratingTemplate, setIsGeneratingTemplate] = React.useState(false);
  const [generatedTemplateStructure, setGeneratedTemplateStructure] = React.useState<any[] | null>(null);
  const [generatedTemplateName, setGeneratedTemplateName] = React.useState<string>('');
  const [isSavingTemplate, setIsSavingTemplate] = React.useState(false);
  
  // Edit template state
  const [editingTemplateId, setEditingTemplateId] = React.useState<string | null>(null);
  const [editingTemplateName, setEditingTemplateName] = React.useState<string>('');
  const [editingTemplateDescription, setEditingTemplateDescription] = React.useState<string>('');
  const [editingTemplateSections, setEditingTemplateSections] = React.useState<any[]>([]);
  const [isSavingEditedTemplate, setIsSavingEditedTemplate] = React.useState(false);
  const [deletingTemplateId, setDeletingTemplateId] = React.useState<string | null>(null);

  // Detect mobile device
  React.useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);
  const [artifactName, setArtifactName] = React.useState('');
  const [userRequest, setUserRequest] = React.useState('');
  const [creating, setCreating] = React.useState(false);
  const [sectionAutoUpdate, setSectionAutoUpdate] = React.useState<Record<string, boolean>>({});
  const [orchestratorModel, setOrchestratorModel] = React.useState<string>('');
  const [defaultModel, setDefaultModel] = React.useState<string>('');
  const [sectionModels, setSectionModels] = React.useState<Record<string, string>>({});
  const [documentAutoUpdateEnabled, setDocumentAutoUpdateEnabled] = React.useState(false);
  const [autoUpdateTrigger, setAutoUpdateTrigger] = React.useState<'on_new_message' | 'time_based' | 'on_new_document'>('time_based');
  const [autoUpdateScheduleType, setAutoUpdateScheduleType] = React.useState<'daily' | 'weekly'>('daily');
  const [expandedSections, setExpandedSections] = React.useState<Record<string, boolean>>({});
  const [autoUpdateMinNewMessages, setAutoUpdateMinNewMessages] = React.useState<number | undefined>(undefined);
  const [autoUpdateMinContextChange, setAutoUpdateMinContextChange] = React.useState<number | undefined>(undefined);
  const [autoUpdateKeywords, setAutoUpdateKeywords] = React.useState<string>('');
  const [autoUpdateSearchQueryChanged, setAutoUpdateSearchQueryChanged] = React.useState(false);
  const [autoUpdateCheckQuality, setAutoUpdateCheckQuality] = React.useState(false);
  const [autoUpdateMinQualityThreshold, setAutoUpdateMinQualityThreshold] = React.useState<number>(0.7);
  const [autoUpdateNotifyOnUpdate, setAutoUpdateNotifyOnUpdate] = React.useState(false);
  const [autoUpdateScheduleTime, setAutoUpdateScheduleTime] = React.useState<string>('09:00');
  const [showContextTriggers, setShowContextTriggers] = React.useState(false);
  const [showConditions, setShowConditions] = React.useState(false);
  const [isCollectingSignals, setIsCollectingSignals] = React.useState(false);
  const [collectedSignals, setCollectedSignals] = React.useState<any>(null);
  const [averageTimingSeconds, setAverageTimingSeconds] = React.useState<number | null>(null);
  const [elapsedSeconds, setElapsedSeconds] = React.useState(0);
  const elapsedTimerRef = React.useRef<number | null>(null);
  
  const genAISettings = useGenAISettings();

  // Function to collect context change signals from discussion
  const handleCollectAutomatically = React.useCallback(async () => {
    if (!projectId || isCollectingSignals) return;
    
    setIsCollectingSignals(true);
    setCollectedSignals(null);
    setElapsedSeconds(0);
    
    // Get average timing for the model
    const provider = genAISettings.provider || 'gemini';
    const { mapProviderStrengthToModel } = await import('../stores/genaiSettingsStore');
    const modelAlias = mapProviderStrengthToModel(provider, 'balanced');
    const modelMapping = mapAliasToModel(modelAlias);
    if (modelMapping) {
      getAverageTiming(modelMapping.provider, modelMapping.model)
        .then((timing) => {
          setAverageTimingSeconds(timing);
        })
        .catch((err) => {
          console.error('[CollectSignals] Failed to get average timing:', err);
          setAverageTimingSeconds(null);
        });
    } else {
      setAverageTimingSeconds(null);
    }
    
    // Start elapsed timer
    elapsedTimerRef.current = window.setInterval(() => {
      setElapsedSeconds((s) => s + 1);
    }, 1000) as unknown as number;
    
    try {
      const chatStore = useGenaiChatStore.getState();
      
      // Get interactions from current project, filter only user and assistant
      const interactions = chatStore.interactions.filter((it: any) => {
        if (!it) return false;
        if (it.projectId !== projectId) return false;
        return it.kind === 'user' || it.kind === 'assistant';
      });
      
      if (interactions.length === 0) {
        setIsCollectingSignals(false);
        if (elapsedTimerRef.current) {
          window.clearInterval(elapsedTimerRef.current);
          elapsedTimerRef.current = null;
        }
        return;
      }
      
      // Sort by createdAt (newest first)
      const sortedInteractions = [...interactions].sort((a, b) => {
        const timeA = new Date(a.createdAt).getTime();
        const timeB = new Date(b.createdAt).getTime();
        return timeB - timeA;
      });
      
      // Build context: first user message, then newest messages (user/assistant) until limit
      const contextMessages: Array<{ role: 'user' | 'assistant'; content: string }> = [];
      const usedIds = new Set<string>();

      const firstUserInteraction = interactions.find((it: any) => it.kind === 'user');
      if (firstUserInteraction) {
        const content = String(firstUserInteraction.content || '').trim();
        if (content) {
          contextMessages.push({ role: 'user', content });
          usedIds.add(firstUserInteraction.id);
        }
      }

      let totalChars = 0;
      const maxChars = 150000 * 4; // 150k tokens (approx)

      for (const it of sortedInteractions) {
        if (usedIds.has(it.id)) continue;

        const content = String(it.content || '').trim();
        if (!content) continue;

        const contentChars = content.length;
        if (totalChars + contentChars > maxChars) {
          const remainingChars = maxChars - totalChars;
          if (remainingChars > 100) {
            contextMessages.push({
              role: it.kind === 'user' ? 'user' : 'assistant',
              content: content.substring(0, remainingChars) + '...',
            });
          }
          break;
        }

        contextMessages.push({
          role: it.kind === 'user' ? 'user' : 'assistant',
          content,
        });
        usedIds.add(it.id);
        totalChars += contentChars;
      }
      
      // Create JSON schema for ListeningSignal structure
      const jsonSchema = {
        type: 'object',
        properties: {
          signals: {
            type: 'array',
            items: {
              type: 'object',
              properties: {
                name: { type: 'string' },
                signal: { type: 'string' },
                searchQueries: {
                  type: 'array',
                  items: { type: 'string' },
                },
                description: { type: 'string' },
              },
              required: ['name', 'signal', 'searchQueries'],
            },
          },
        },
        required: ['signals'],
      };
      
      // Build prompt
      const prompt = `You are analyzing a discussion to identify context change signals - data points that are stable enough to track but potentially changeable over time.

Your task:
1. Analyze the discussion conversation below
2. Identify stable data points that represent potentially changeable information (e.g., metrics, statuses, decisions, plans, market conditions, etc.)
3. For each identified data point, create a signal with:
   - A descriptive name
   - The signal text (what to listen for)
   - Search queries that would help detect changes to this signal
   - An optional description

Focus on:
- Metrics and numbers that may change (e.g., "market share", "user count", "revenue target")
- Statuses and states that may evolve (e.g., "project status", "partnership status", "compliance status")
- Decisions and plans that may be updated (e.g., "product roadmap", "hiring plan", "expansion strategy")
- Market conditions and trends (e.g., "competitor activity", "regulatory changes", "technology adoption")

Be thoughtful and selective - only include signals that are:
- Clearly discussed in the conversation
- Stable enough to be tracked over time
- Potentially changeable (not static facts)

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${JSON.stringify(jsonSchema, null, 2)}

DISCUSSION:
${contextMessages.map((m, i) => `${m.role === 'user' ? 'User' : 'Assistant'}: ${m.content}`).join('\n\n')}`;
      
      // Get model from settings
      const provider = genAISettings.provider || 'gemini';
      const { mapProviderStrengthToModel } = await import('../stores/genaiSettingsStore');
      const model = mapProviderStrengthToModel(provider, 'balanced');
      
      // Call LLM with structured output
      const res = await api.genaiGenerate({
        model: model,
        prompt: prompt,
        strictJSONEnabled: true,
        jsonSchema: jsonSchema,
        maxOutputTokens: 4096,
        webSearchEnabled: false,
        projectId: projectId,
      });
      
      // Parse response
      let parsed: any = null;
      try {
        if (typeof res?.content === 'string') {
          const s = res.content.trim();
          if (s.startsWith('{') || s.startsWith('[')) parsed = JSON.parse(s);
        }
        const rawJSON = (res as any)?.rawJSON;
        if (!parsed && typeof rawJSON === 'string') {
          const s2 = rawJSON.trim();
          if (s2.startsWith('{') || s2.startsWith('[')) parsed = JSON.parse(s2);
        }
        if (!parsed && typeof res?.content === 'string') {
          const m = res.content.match(/\{[\s\S]*\}/);
          if (m && m[0]) {
            parsed = JSON.parse(m[0]);
          }
        }
      } catch (pe) {
        console.warn('[CollectSignals] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
      }
      
      if (parsed && parsed.signals && Array.isArray(parsed.signals)) {
        console.log('[CollectSignals] Successfully collected signals:', parsed);
        setCollectedSignals(parsed);
      } else {
        console.warn('[CollectSignals] Failed to parse signals:', { parsed, raw: res });
        setCollectedSignals({ error: 'Failed to parse signals', raw: res });
      }
    } catch (error) {
      console.error('[CollectSignals] Failed to collect signals', error);
      setCollectedSignals({ error: String(error) });
    } finally {
      setIsCollectingSignals(false);
      if (elapsedTimerRef.current) {
        window.clearInterval(elapsedTimerRef.current);
        elapsedTimerRef.current = null;
      }
    }
  }, [projectId, isCollectingSignals, genAISettings]);
  
  // Cleanup timer on unmount
  React.useEffect(() => {
    return () => {
      if (elapsedTimerRef.current) {
        window.clearInterval(elapsedTimerRef.current);
        elapsedTimerRef.current = null;
      }
    };
  }, []);

  // Load artifacts
  React.useEffect(() => {
    if (!projectId || projectId === 'local-unfold') {
      setArtifacts([]);
      return;
    }

    const loadArtifacts = async () => {
      setLoading(true);
      try {
        const docs = await api.docsList(projectId);
        const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
        setArtifacts(artifactsList);
      } catch (err) {
        console.error('[DynamicContentPanel] Failed to load artifacts', err);
        setArtifacts([]);
      } finally {
        setLoading(false);
      }
    };

    loadArtifacts();
  }, [projectId]);

  // Focus input when editing starts
  React.useEffect(() => {
    if (editingArtifactId && editingArtifactInputRef.current) {
      editingArtifactInputRef.current.focus();
      editingArtifactInputRef.current.select();
    }
  }, [editingArtifactId]);

  // Load templates
  React.useEffect(() => {
    const loadTemplates = async () => {
      setTemplatesLoading(true);
      try {
        const templatesList = await api.listArtifactTemplates();
        console.log('[DynamicContentPanel] Loaded templates:', templatesList);
        console.log('[DynamicContentPanel] Templates with type custom:', templatesList?.filter((t: any) => t.type === 'custom'));
        setTemplates(templatesList || []);
      } catch (err) {
        console.error('[DynamicContentPanel] Failed to load templates', err);
        setTemplates([]);
      } finally {
        setTemplatesLoading(false);
      }
    };

    loadTemplates();
  }, []);

  const getArtifactIcon = () => (
    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );

  const getTemplateIcon = () => (
    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 2v6h6M12 18v-6M9 15h6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );

  const getFolderIcon = () => (
    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return '';
    try {
      const date = new Date(dateStr);
      return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
    } catch {
      return '';
    }
  };

  // Separate templates into system and custom
  const { systemTemplates, customTemplates } = React.useMemo(() => {
    const system: ArtifactTemplate[] = [];
    const custom: ArtifactTemplate[] = [];
    
    templates.forEach(template => {
      console.log('[DynamicContentPanel] Template:', { id: template.id, name: template.name, type: template.type, hasType: 'type' in template });
      if (template.type === 'system') {
        system.push(template);
      } else if (template.type === 'custom') {
        custom.push(template);
      } else {
        console.warn('[DynamicContentPanel] Template without recognized type:', template);
      }
    });
    
    console.log('[DynamicContentPanel] Separated templates:', { system: system.length, custom: custom.length });
    return { systemTemplates: system, customTemplates: custom };
  }, [templates]);

  // Group system templates by category
  const groupedSystemTemplates = React.useMemo(() => {
    const grouped = systemTemplates.reduce((acc, template) => {
      const category = template.category || 'Other';
      if (!acc[category]) {
        acc[category] = [];
      }
      acc[category].push(template);
      return acc;
    }, {} as Record<string, ArtifactTemplate[]>);

    return Object.entries(grouped)
      .map(([category, categoryTemplates]) => ({
        category,
        templates: categoryTemplates.sort((a, b) => 
          a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
        ),
      }))
      .sort((a, b) => 
        a.category.localeCompare(b.category, undefined, { sensitivity: 'base' })
      );
  }, [systemTemplates]);

  // Group custom templates by category
  const groupedCustomTemplates = React.useMemo(() => {
    const grouped = customTemplates.reduce((acc, template) => {
      const category = template.category || 'Other';
      if (!acc[category]) {
        acc[category] = [];
      }
      acc[category].push(template);
      return acc;
    }, {} as Record<string, ArtifactTemplate[]>);

    return Object.entries(grouped)
      .map(([category, categoryTemplates]) => ({
        category,
        templates: categoryTemplates.sort((a, b) => 
          a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
        ),
      }))
      .sort((a, b) => 
        a.category.localeCompare(b.category, undefined, { sensitivity: 'base' })
      );
  }, [customTemplates]);

  const handleTemplateSelect = async (templateId: string) => {
    try {
      const fullTemplate = await api.getArtifactTemplate(templateId);
      setSelectedTemplate(fullTemplate);
      setWizardStep('configure');
      setArtifactName(fullTemplate.name || '');
      setUserRequest('');
      
      // Initialize models from template recommendations
      const recommendedOrch = fullTemplate.recommendedOrchestratorModel || 'gemini-fast';
      const recommendedSection = fullTemplate.recommendedSectionModel || 'gemini-fast';
      // Normalize to underscore format for consistency
      setOrchestratorModel(recommendedOrch.replace(/-/g, '_'));
      setDefaultModel(recommendedSection.replace(/-/g, '_'));
      
      // Initialize auto-update settings for all sections (default: false)
      const initialAutoUpdate: Record<string, boolean> = {};
      const initialSectionModels: Record<string, string> = {};
      (fullTemplate.defaultStructure || []).forEach((section: { id: string }) => {
        initialAutoUpdate[section.id] = false;
        initialSectionModels[section.id] = recommendedSection.replace(/-/g, '_');
      });
      setSectionAutoUpdate(initialAutoUpdate);
      setSectionModels(initialSectionModels);
      
      // Initialize document auto-update (default: false)
      setDocumentAutoUpdateEnabled(false);
      setAutoUpdateTrigger('time_based');
      setAutoUpdateScheduleType('daily');
      setShowContextTriggers(false);
      setShowConditions(false);
      setAutoUpdateMinNewMessages(undefined);
      setAutoUpdateMinContextChange(undefined);
      setAutoUpdateKeywords('');
      setAutoUpdateSearchQueryChanged(false);
      setAutoUpdateCheckQuality(false);
      setAutoUpdateMinQualityThreshold(0.7);
      setAutoUpdateNotifyOnUpdate(false);
      
      // Initialize expanded sections (default: all collapsed)
      setExpandedSections({});
    } catch (err) {
      console.error('[DynamicContentPanel] Failed to load template', err);
    }
  };

  const handleBack = () => {
    if (wizardStep === 'preview') {
      setWizardStep('configure');
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
    } else if (wizardStep === 'configure') {
      // If it's a generated template, go back to template creation wizard
      if (selectedTemplate?.id === 'generated-template') {
        setWizardStep('templates');
        setShowCreateTemplateWizard(true);
        setSelectedTemplate(null);
        setArtifactName('');
        setUserRequest(templateUserRequest); // Keep the user request
      } else {
      setWizardStep('templates');
      setSelectedTemplate(null);
      setArtifactName('');
      setUserRequest('');
      }
      setSectionAutoUpdate({});
      setOrchestratorModel('');
      setDefaultModel('');
      setSectionModels({});
      setDocumentAutoUpdateEnabled(false);
      setAutoUpdateTrigger('time_based');
      setAutoUpdateScheduleType('daily');
      setShowContextTriggers(false);
      setShowConditions(false);
      setAutoUpdateMinNewMessages(undefined);
      setAutoUpdateMinContextChange(undefined);
      setAutoUpdateKeywords('');
      setAutoUpdateSearchQueryChanged(false);
      setAutoUpdateCheckQuality(false);
      setAutoUpdateMinQualityThreshold(0.7);
      setAutoUpdateNotifyOnUpdate(false);
      setExpandedSections({});
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
      setGeneratedTemplateStructure(null);
    }
  };

  // Generate content for all sections
  const handleGeneratePreview = async () => {
    if (!projectId || projectId === 'local-unfold' || !selectedTemplate || !artifactName.trim()) {
      return;
    }

    setGenerating(true);
    setGeneratedSections({});
    
    const sortedSections = [...(selectedTemplate.defaultStructure || [])].sort(
      (a, b) => a.order - b.order
    );
    
    setGenerationProgress({ current: 0, total: sortedSections.length });
    const sectionsContent: Record<string, { content: string; error?: string }> = {};

    // Get chat context and preferred language
    const chatStore = useGenaiChatStore.getState();
    const preferredLang = getPreferredLanguage();
    const langName = getLanguageName(preferredLang);
    
    // Build chat context from interactions (prioritize interactions, fallback to messages)
    // Only use interactions from the current project
    const chatContext: Array<{ role: string; content: string }> = [];
    const interactions = chatStore.interactions.filter((it: any) => {
      if (!it) return false;
      // Only include interactions from current project
      if (it.projectId !== projectId) return false;
      // Exclude ontology interactions (they are structured data, not chat content)
      if (it.kind === 'ontology') {
        return false;
      }
      // Exclude layout interactions (UI state, not chat content)
      if (it.kind === 'ontology' && it.subkind === 'layout') {
        return false;
      }
      // Exclude other non-chat interactions
      if (it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end') {
        return false;
      }
      // Exclude perspective interactions
      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
        return false;
      }
      // Exclude queued tasks
      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
        return false;
      }
      // Exclude agent execution interactions (they are execution status, not chat content)
      if (it.kind === 'agent.execution') {
        return false;
      }
      // Only include user and assistant interactions (actual chat content)
      return it.kind === 'user' || it.kind === 'assistant';
    });
    
    if (interactions.length > 0) {
      interactions.forEach((it: any) => {
        const content = String(it.content || '').trim();
        if (content) {
          chatContext.push({
            role: it.kind === 'user' ? 'user' : 'assistant',
            content: content,
          });
        }
      });
    } else {
      // Fallback to messages if no interactions
      const messages = chatStore.messages || [];
      messages.forEach((m: any) => {
        const content = String(m?.content || '').trim();
        if (content) {
          chatContext.push({
            role: String(m?.role || '').toLowerCase() === 'user' ? 'user' : 'assistant',
            content: content,
          });
        }
      });
    }
    
    // Build context block for prompt (last 10 messages for context)
    const recentContext = chatContext.slice(-10);
    const contextBlock = recentContext.length > 0
      ? `\n\nCHAT CONTEXT (recent conversation):\n${recentContext.map((m) => `${m.role}: ${m.content}`).join('\n')}`
      : '';

    try {
      // Generate content for each section sequentially
      for (let i = 0; i < sortedSections.length; i++) {
        const section = sortedSections[i];
        const sectionModel = sectionModels[section.id] || defaultModel;
        
        setGenerationProgress({ current: i + 1, total: sortedSections.length });
        
        try {
          // Build prompt with context
          const promptWithContext = section.promptTemplate + contextBlock;
          
          // Build system instruction with language preference, current date, and professional requirements
          const currentDate = new Date().toLocaleDateString('en-US', { 
            year: 'numeric', 
            month: 'long', 
            day: 'numeric',
            timeZone: 'UTC'
          });
          const langDirective = `You are preparing a section of a professional document. Your task is to generate the actual content of this section, not to comment on it or discuss it.

You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.

Current date: ${currentDate}

CRITICAL REQUIREMENTS:
- You are preparing a document section (or complete document). Output ONLY the actual content, not meta-commentary or conversational phrases
- ABSOLUTELY FORBIDDEN phrases: "Отлично, теперь мы сделаем...", "Согласен, ...", "Хорошо, ...", "Excellent, now we will...", "I agree, ...", "Let's...", "We will now...", or any similar conversational or meta-commentary phrases
- Do NOT address the user, reader, or any person directly in the document or sections
- Do NOT use second person ("you", "your") or first person plural ("we", "our") unless referring to a specific entity mentioned in the context
- Do NOT use phrases that suggest you are in a conversation or responding to someone (e.g., "As requested...", "As you can see...", "Let me explain...")
- Prepare content exclusively as professional, standalone documentation that reads as if it was written directly, not generated
- Write in an objective, third-person style appropriate for formal documentation
- Start directly with the content. Do not introduce it with phrases like "This section covers..." unless that is the actual document structure
- Focus on factual, informative content without personal references, direct appeals, or conversational elements

FORMAT REQUIREMENTS:
- Output MUST be strictly in Markdown format only
- If a section requires examples, code snippets, data structures, JSON, XML, or any structured content, it MUST be placed inside Markdown code fences (triple backticks with language identifier)
- NEVER output raw JSON, XML, or other structured formats outside of code fences
- Use proper Markdown syntax: headers (#), lists (- or *), bold (**text**), italic (*text*), links, tables, etc.
- Code examples must use appropriate language identifiers in code fences (e.g., \`\`\`json, \`\`\`python, \`\`\`yaml)`;
          
          const response = await api.genaiGenerate({
            model: sectionModel.replace(/_/g, '-'),
            prompt: promptWithContext,
            systemInstruction: langDirective,
            maxOutputTokens: 4096,
            webSearchEnabled: false,
          });
          
          sectionsContent[section.id] = {
            content: response.content || '',
          };
        } catch (err: any) {
          console.error(`[DynamicContentPanel] Failed to generate section ${section.id}`, err);
          sectionsContent[section.id] = {
            content: '',
            error: err.message || 'Failed to generate content',
          };
        }
        
        // Update UI progressively
        setGeneratedSections({ ...sectionsContent });
      }
      
      // Move to preview step
      setWizardStep('preview');
    } catch (err) {
      console.error('[DynamicContentPanel] Failed to generate preview', err);
    } finally {
      setGenerating(false);
    }
  };

  // Refresh a single section
  const handleRefreshSection = async (sectionId: string) => {
    if (!projectId || projectId === 'local-unfold' || !selectedTemplate) {
      return;
    }

    const section = selectedTemplate.defaultStructure?.find(s => s.id === sectionId);
    if (!section) {
      return;
    }

    setRefreshingSectionId(sectionId);

    try {
      // Get chat context and preferred language
      const chatStore = useGenaiChatStore.getState();
      const preferredLang = getPreferredLanguage();
      const langName = getLanguageName(preferredLang);
      
      // Build chat context from interactions (prioritize interactions, fallback to messages)
      const chatContext: Array<{ role: string; content: string }> = [];
      const interactions = chatStore.interactions.filter((it: any) => {
        if (!it) return false;
        if (it.projectId !== projectId) return false;
        return it.kind === 'user' || it.kind === 'assistant';
      });
      
      if (interactions.length > 0) {
        interactions.forEach((it: any) => {
          const content = String(it.content || '').trim();
          if (content) {
            chatContext.push({
              role: it.kind === 'user' ? 'user' : 'assistant',
              content: content,
            });
          }
        });
      } else {
        const messages = chatStore.messages || [];
        messages.forEach((m: any) => {
          const content = String(m?.content || '').trim();
          if (content) {
            chatContext.push({
              role: String(m?.role || '').toLowerCase() === 'user' ? 'user' : 'assistant',
              content: content,
            });
          }
        });
      }
      
      // Build context block for prompt (last 10 messages for context)
      const recentContext = chatContext.slice(-10);
      const contextBlock = recentContext.length > 0
        ? `\n\nCHAT CONTEXT (recent conversation):\n${recentContext.map((m) => `${m.role}: ${m.content}`).join('\n')}`
        : '';

      // Build prompt with context
      const promptWithContext = section.promptTemplate + contextBlock;
      
      // Build system instruction
      const currentDate = new Date().toLocaleDateString('en-US', { 
        year: 'numeric', 
        month: 'long', 
        day: 'numeric',
        timeZone: 'UTC'
      });
      const langDirective = `You are preparing a section of a professional document. Your task is to generate the actual content of this section, not to comment on it or discuss it.

You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.

Current date: ${currentDate}

CRITICAL REQUIREMENTS:
- You are preparing a document section (or complete document). Output ONLY the actual content, not meta-commentary or conversational phrases
- ABSOLUTELY FORBIDDEN phrases: "Отлично, теперь мы сделаем...", "Согласен, ...", "Хорошо, ...", "Excellent, now we will...", "I agree, ...", "Let's...", "We will now...", or any similar conversational or meta-commentary phrases
- Do NOT address the user, reader, or any person directly in the document or sections
- Do NOT use second person ("you", "your") or first person plural ("we", "our") unless referring to a specific entity mentioned in the context
- Do NOT use phrases that suggest you are in a conversation or responding to someone (e.g., "As requested...", "As you can see...", "Let me explain...")
- Prepare content exclusively as professional, standalone documentation that reads as if it was written directly, not generated
- Write in an objective, third-person style appropriate for formal documentation
- Start directly with the content. Do not introduce it with phrases like "This section covers..." unless that is the actual document structure
- Focus on factual, informative content without personal references, direct appeals, or conversational elements

FORMAT REQUIREMENTS:
- Output MUST be strictly in Markdown format only
- If a section requires examples, code snippets, data structures, JSON, XML, or any structured content, it MUST be placed inside Markdown code fences (triple backticks with language identifier)
- NEVER output raw JSON, XML, or other structured formats outside of code fences
- Use proper Markdown syntax: headers (#), lists (- or *), bold (**text**), italic (*text*), links, tables, etc.
- Code examples must use appropriate language identifiers in code fences (e.g., \`\`\`json, \`\`\`python, \`\`\`yaml)`;
      
      const sectionModel = sectionModels[sectionId] || defaultModel;
      const response = await api.genaiGenerate({
        model: sectionModel.replace(/_/g, '-'),
        prompt: promptWithContext,
        systemInstruction: langDirective,
        maxOutputTokens: 4096,
        webSearchEnabled: false,
      });
      
      setGeneratedSections((prev) => ({
        ...prev,
        [sectionId]: {
          content: response.content || '',
        },
      }));
    } catch (err: any) {
      console.error(`[DynamicContentPanel] Failed to refresh section ${sectionId}`, err);
      setGeneratedSections((prev) => ({
        ...prev,
        [sectionId]: {
          content: prev[sectionId]?.content || '',
          error: err.message || 'Failed to generate content',
        },
      }));
    } finally {
      setRefreshingSectionId(null);
    }
  };

  const handleSaveArtifact = async () => {
    if (!projectId || projectId === 'local-unfold' || !selectedTemplate || !artifactName.trim()) {
      return;
    }

    setCreating(true);
    try {
      // Convert template structure to artifact structure with generated content
      const artifactSections = (selectedTemplate.defaultStructure || []).map((section) => {
        const sectionModel = sectionModels[section.id] || defaultModel;
        const generated = generatedSections[section.id];
        return {
          id: section.id,
          title: section.title,
          order: section.order,
          prompt: section.promptTemplate,
          model: sectionModel !== defaultModel ? sectionModel.replace(/_/g, '-') : undefined,
          description: section.description,
          required: section.required,
          editable: section.editable,
          autoUpdateEnabled: sectionAutoUpdate[section.id] || false,
          content: generated?.content || '',
          generatedAt: generated?.content ? new Date().toISOString() : undefined,
        };
      });

      // Generate version ID
      const versionId = `v_${Date.now()}`;

      // Build auto-update config if enabled
      const conditions: any = {};
      if (autoUpdateMinNewMessages !== undefined && autoUpdateMinNewMessages > 0) {
        conditions.minNewMessages = autoUpdateMinNewMessages;
      }
      if (autoUpdateMinContextChange !== undefined && autoUpdateMinContextChange > 0) {
        conditions.minContextChange = autoUpdateMinContextChange;
      }
      if (autoUpdateKeywords.trim()) {
        conditions.keywords = autoUpdateKeywords.split(',').map(k => k.trim()).filter(Boolean);
      }
      if (autoUpdateSearchQueryChanged) {
        conditions.searchQueryChanged = true;
      }

      const smartUpdate = autoUpdateCheckQuality ? {
        checkQuality: true,
        minQualityThreshold: autoUpdateMinQualityThreshold,
        notifyOnUpdate: autoUpdateNotifyOnUpdate,
      } : undefined;

      // For time_based trigger, use scheduleType as the actual trigger (daily/weekly)
      const actualTrigger = autoUpdateTrigger === 'time_based' ? autoUpdateScheduleType : autoUpdateTrigger;

      const autoUpdateConfig = documentAutoUpdateEnabled ? {
        trigger: actualTrigger,
        scheduleTime: autoUpdateTrigger === 'time_based' ? autoUpdateScheduleTime : undefined,
        conditions: Object.keys(conditions).length > 0 ? conditions : undefined,
        smartUpdate: smartUpdate,
      } : undefined;

      const artifactDoc: Partial<CoreDoc> = {
        filename: artifactName.trim(),
        contentType: 'text/markdown',
        docType: 'dynamic_artifact',
        artifactConfig: {
          format: (selectedTemplate.recommendedFormat || 'text') as 'text' | 'table' | 'report' | 'presentation',
          focus: (selectedTemplate.recommendedFocus || 'analysis') as 'brief' | 'analysis' | 'narrative' | 'proposal' | 'technical' | 'executive',
          orchestratorModel: orchestratorModel.replace(/_/g, '-'),
          defaultModel: defaultModel.replace(/_/g, '-'),
          reviewPrompt: 'Review and refine for clarity, consistency, and completeness.',
          structure: artifactSections,
          contextSources: {},
          autoUpdateEnabled: documentAutoUpdateEnabled,
          autoUpdateConfig: autoUpdateConfig,
          currentVersionID: versionId,
          versionsRetained: 10,
          versions: [],
          generationSettings: {
            preferredLanguage: 'en',
            systemPrompt: 'You are a helpful assistant.',
          },
          userRequest: userRequest.trim() || undefined,
        },
      };

      const createdDoc = await api.docsCreate(projectId, artifactDoc as any);
      
      // Reload artifacts list
      const docs = await api.docsList(projectId);
      const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
      setArtifacts(artifactsList);
      
      // Dispatch event to notify SidebarPanel to refresh
      window.dispatchEvent(new CustomEvent('content.updated', { detail: { projectId } }));

      // Reset wizard and open the created document
      setWizardStep('templates');
      setSelectedTemplate(null);
      setArtifactName('');
      setUserRequest('');
      setSectionAutoUpdate({});
      setOrchestratorModel('');
      setDefaultModel('');
      setSectionModels({});
      setDocumentAutoUpdateEnabled(false);
      setAutoUpdateTrigger('time_based');
      setAutoUpdateScheduleType('daily');
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
      onOpenDocument(createdDoc);
    } catch (err) {
      console.error('[DynamicContentPanel] Failed to create artifact', err);
    } finally {
      setCreating(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="animate-pulse text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
          Loading content...
        </div>
      </div>
    );
  }

  // Welcome component
  const WelcomeMessage = ({ template }: { template: NonNullable<typeof selectedTemplate> }) => (
    <div
      className="px-3 py-2.5 rounded-lg"
      style={{
        backgroundColor: 'var(--toolbar-bg)',
        border: '1px solid var(--overlay-line-90)',
      }}
    >
      <p
        className="text-xs mb-2"
        style={{ color: 'var(--text-heading)' }}
      >
        Welcome to <strong>{template.name}</strong> builder.
      </p>
      {template.description && (
        <p
          className="text-[10px] leading-relaxed"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          {template.description}
        </p>
      )}
    </div>
  );

  // TemplateDisplay component - displays template name and sections
  const TemplateDisplay = ({ template }: { template: ArtifactTemplate }) => {
    const sortedSections = [...(template.defaultStructure || [])].sort((a, b) => a.order - b.order);
    
    return (
      <div>
        <div className="mb-2">
          <h5 className="text-xs font-medium mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
            Template Name
          </h5>
          <h4 className="text-base font-semibold" style={{ color: 'var(--text-heading)' }}>
            {template.name}
          </h4>
          {template.description && (
            <p className="text-xs mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
              {template.description}
            </p>
          )}
        </div>
        
        {sortedSections.length > 0 && (
          <div>
            <label className="block text-13 mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
              Sections
            </label>
            <div className="space-y-2">
              {sortedSections.map((section) => {
                const isExpanded = expandedSections[section.id] || false;
                return (
                  <div
                    key={section.id}
                    className="px-3 py-2 rounded"
                    style={{
                      backgroundColor: 'var(--white-06)',
                      border: '1px solid var(--overlay-line-90)',
                    }}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setExpandedSections((prev) => ({
                          ...prev,
                          [section.id]: !prev[section.id],
                        }));
                      }}
                      className="w-full flex items-center justify-between text-left"
                    >
                      <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
                        {section.title}
                        {section.required && (
                          <span className="ml-1 text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                            (required)
                          </span>
                        )}
                      </div>
                      <svg
                        className="w-3 h-3 transition-transform"
                        style={{
                          transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                          color: 'var(--text-secondary)',
                        }}
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                      >
                        <path d="M6 9l6 6 6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </button>
                    {isExpanded && (
                      <>
                        {section.description && (
                          <div className="text-[10px] mb-1 mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                            {section.description}
                          </div>
                        )}
                        {section.promptTemplate && (
                          <div
                            className="text-[10px] mt-1 pt-1"
                            style={{
                              color: 'var(--text-tertiary-weak)',
                              borderTop: '1px solid var(--overlay-line-90)',
                              fontStyle: 'italic',
                            }}
                          >
                            {section.promptTemplate}
                          </div>
                        )}
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    );
  };

  // Templates list component (reusable)
  const TemplatesList = ({ 
    compact = false, 
    selectedTemplateId,
    onTemplateSelect,
    templates: templatesToShow,
    groupedTemplates: groupedTemplatesToShow,
    widerColumns = false,
    isCustomTemplates = false,
    onEditTemplate,
    onDeleteTemplate,
    editingTemplateId,
    deletingTemplateId,
  }: { 
    compact?: boolean;
    selectedTemplateId?: string | null;
    onTemplateSelect: (templateId: string) => void;
    templates: ArtifactTemplate[];
    groupedTemplates: Array<{ category: string; templates: ArtifactTemplate[] }>;
    widerColumns?: boolean;
    isCustomTemplates?: boolean;
    onEditTemplate?: (template: ArtifactTemplate) => void;
    onDeleteTemplate?: (templateId: string) => void;
    editingTemplateId?: string | null;
    deletingTemplateId?: string | null;
  }) => {
    if (templatesLoading) {
      return (
        <div
          className="text-xs py-2 text-center"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          Loading templates...
        </div>
      );
    }
    if (templatesToShow.length === 0) {
      return null; // Empty state handled by parent
    }

    if (compact) {
      // Compact vertical list for desktop sidebar
      return (
        <div className="space-y-1">
          {groupedTemplatesToShow.map(({ category, templates: categoryTemplates }) => (
            <div key={category}>
              <div
                className="text-[10px] uppercase tracking-wide mb-1 px-2"
                style={{ color: 'var(--text-tertiary-weak)' }}
              >
                {category}
              </div>
              <div className="space-y-0.5">
                {categoryTemplates.map((template) => (
                  <div
                    key={template.id}
                    className="px-2 py-1.5 rounded text-xs cursor-pointer"
                    style={{
                      backgroundColor: selectedTemplateId === template.id
                        ? 'var(--white-10)'
                        : 'transparent',
                      border: selectedTemplateId === template.id
                        ? '1px solid var(--overlay-line-90)'
                        : 'none',
                    }}
                    onMouseEnter={(e) => {
                      if (selectedTemplateId !== template.id) {
                        (e.currentTarget as HTMLDivElement).style.backgroundColor =
                          'var(--white-06)';
                      }
                    }}
                    onMouseLeave={(e) => {
                      if (selectedTemplateId !== template.id) {
                        (e.currentTarget as HTMLDivElement).style.backgroundColor =
                          'transparent';
                      }
                    }}
                    onClick={() => onTemplateSelect(template.id)}
                  >
                    <div
                      className="font-medium truncate"
                      style={{ color: 'var(--text-heading)' }}
                      title={template.name}
                    >
                      {template.name}
                      {template.isFeatured && (
                        <span
                          className="ml-1 text-[10px]"
                          style={{ color: 'var(--accent-primary)' }}
                        >
                          ★
                        </span>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      );
    }

    // Full list for mobile or templates view
    return (
      <div className="space-y-4">
        {groupedTemplatesToShow.map(({ category, templates: categoryTemplates }) => (
          <div key={category}>
            <div
              className="text-[10px] uppercase tracking-wide mb-1.5"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              {category}
            </div>
            <div 
              className="grid gap-2"
              style={{
                gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
              }}
            >
              {categoryTemplates.map((template) => {
                const isEditing = editingTemplateId === template.id;
                const isDeleting = deletingTemplateId === template.id;
                
                return (
                  <div key={template.id}>
                    <div
                      className="px-2 py-1.5 rounded text-xs flex items-start gap-2 group"
              style={{
                    backgroundColor: isEditing ? 'var(--white-10)' : 'transparent',
                        opacity: isDeleting ? 0.5 : 1,
                  }}
                  onMouseEnter={(e) => {
                        if (!isEditing && !isDeleting) {
                    (e.currentTarget as HTMLDivElement).style.backgroundColor =
                      'var(--white-10)';
                        }
                  }}
                  onMouseLeave={(e) => {
                        if (!isEditing) {
                    (e.currentTarget as HTMLDivElement).style.backgroundColor =
                      'transparent';
                        }
                  }}
                >
                  <div className="flex-shrink-0 mt-0.5" style={{ color: 'var(--text-secondary)' }}>
                    {getTemplateIcon()}
                  </div>
                      <div className="min-w-0 flex-1">
                    <div
                          className="font-medium cursor-pointer"
                      style={{ color: 'var(--text-heading)' }}
                      title={template.name}
                          onClick={() => !isEditing && !isDeleting && onTemplateSelect(template.id)}
                    >
                      {template.name}
                      {template.isFeatured && (
                        <span
                          className="ml-1 text-[10px]"
                          style={{ color: 'var(--accent-primary)' }}
                        >
                          ★
                        </span>
                      )}
                    </div>
                    {template.description && (
                      <div
                        className="text-[10px] mt-0.5 line-clamp-2"
                        style={{ 
                          color: 'var(--text-secondary-strong)',
                        }}
                        title={template.description}
                      >
                        {template.description}
                      </div>
                    )}
                    {template.category && (
                      <div className="mt-1.5 inline-block">
                        <span
                          className="text-[10px] px-2 py-0.5 rounded-full"
                          style={{ 
                            color: 'var(--text-secondary-strong)',
                            backgroundColor: 'var(--white-10)',
                          }}
                        >
                          {template.category}
                        </span>
                      </div>
                    )}
                  </div>
                      {isCustomTemplates && !isEditing && !isDeleting && (
                        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                          <button
                            className="p-1 rounded"
                            title={t('artifacts.editTemplate', { defaultValue: 'Edit template' })}
                            style={{ backgroundColor: 'transparent' }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                            onClick={(e) => {
                              e.stopPropagation();
                              if (onEditTemplate) {
                                onEditTemplate(template);
                              }
                            }}
                          >
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                              />
                            </svg>
                          </button>
                          <button
                            className="p-1 rounded"
                            title={t('artifacts.deleteTemplate', { defaultValue: 'Delete template' })}
                            style={{ backgroundColor: 'transparent' }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                            onClick={(e) => {
                              e.stopPropagation();
                              if (onDeleteTemplate) {
                                onDeleteTemplate(template.id);
                              }
                            }}
                          >
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                              />
                            </svg>
                          </button>
                </div>
                      )}
                    </div>
                    
                    {/* Edit form */}
                    {isEditing && (
                      <EditTemplateForm
                        template={template}
                        onSave={async (name, description, sections) => {
                          await api.updateArtifactTemplate(template.id, {
                            name,
                            description,
                            defaultStructure: sections.map((section: any) => ({
                              id: section.id,
                              title: section.title,
                              order: section.order,
                              description: section.description || '',
                              promptTemplate: section.promptTemplate || section.prompt || '',
                              required: section.required !== undefined ? section.required : true,
                              editable: section.editable !== undefined ? section.editable : true,
                            })),
                          });
                          
                          // Reload templates
                          const templatesList = await api.listArtifactTemplates();
                          setTemplates(templatesList);
                          
                          // Reset editing state
                          setEditingTemplateId(null);
                          setEditingTemplateName('');
                          setEditingTemplateDescription('');
                          setEditingTemplateSections([]);
                          
                          alert('Template updated successfully!');
                        }}
                        onCancel={() => {
                          setEditingTemplateId(null);
                          setEditingTemplateName('');
                          setEditingTemplateDescription('');
                          setEditingTemplateSections([]);
                        }}
                      />
                    )}
                    {false && isEditing && (
                      <div 
                        className="mt-3 pt-3 pb-3 border-t border-b" 
                        style={{ borderColor: 'var(--overlay-line-90)' }}
                        onClick={(e) => e.stopPropagation()}
                        onMouseDown={(e) => e.stopPropagation()}
                      >
                        <div className="space-y-4">
                          {/* Template Name */}
                          <div>
                            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                              Template Name
                            </label>
                            <input
                              type="text"
                              value={editingTemplateName}
                              onChange={(e) => {
                                e.stopPropagation();
                                setEditingTemplateName(e.target.value);
                              }}
                              onFocus={(e) => e.stopPropagation()}
                              onClick={(e) => e.stopPropagation()}
                              className="w-full px-2 py-1.5 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                              }}
                            />
                          </div>
                          
                          {/* Template Description */}
                          <div>
                            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                              Description
                            </label>
                            <textarea
                              value={editingTemplateDescription}
                              onChange={(e) => {
                                e.stopPropagation();
                                setEditingTemplateDescription(e.target.value);
                              }}
                              onFocus={(e) => e.stopPropagation()}
                              onClick={(e) => e.stopPropagation()}
                              rows={2}
                              className="w-full px-2 py-1.5 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                resize: 'vertical',
                              }}
                            />
                          </div>
                          
                          {/* Sections */}
                          {editingTemplateSections.length > 0 && (
                            <div>
                              <label className="block text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
                                Sections
                              </label>
                              <div className="space-y-2">
                                {editingTemplateSections.map((section: any, index: number) => {
                                  const isExpanded = expandedSections[`edit-${section.id}`] || false;
                                  return (
                                    <div
                                      key={section.id}
                                      className="px-3 py-2 rounded"
                                      style={{
                                        backgroundColor: 'var(--white-06)',
                                        border: '1px solid var(--overlay-line-90)',
                                      }}
                                    >
                                      <button
                                        type="button"
                                        onClick={() => {
                                          setExpandedSections((prev) => ({
                                            ...prev,
                                            [`edit-${section.id}`]: !prev[`edit-${section.id}`],
                                          }));
                                        }}
                                        className="w-full flex items-center justify-between text-left mb-2"
                                      >
                                        <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
                                          {section.title}
                                          {section.required && (
                                            <span className="ml-1 text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                                              (required)
                                            </span>
                                          )}
                                        </div>
                                        <svg
                                          className="w-3 h-3 transition-transform"
                                          style={{
                                            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                                            color: 'var(--text-secondary)',
                                          }}
                                          viewBox="0 0 24 24"
                                          fill="none"
                                          stroke="currentColor"
                                        >
                                          <path d="M6 9l6 6 6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                                        </svg>
                                      </button>
                                      {isExpanded && (
                                        <div 
                                          className="space-y-2 mt-2 pt-2 border-t" 
                                          style={{ borderColor: 'var(--overlay-line-90)' }}
                                          onClick={(e) => e.stopPropagation()}
                                          onMouseDown={(e) => e.stopPropagation()}
                                        >
                                          <div>
                                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                              Title
                                            </label>
                                            <input
                                              type="text"
                                              value={section.title}
                                              onChange={(e) => {
                                                e.stopPropagation();
                                                const updated = [...editingTemplateSections];
                                                updated[index].title = e.target.value;
                                                setEditingTemplateSections(updated);
                                              }}
                                              onFocus={(e) => e.stopPropagation()}
                                              onClick={(e) => e.stopPropagation()}
                                              className="w-full px-2 py-1 rounded text-xs"
                                              style={{
                                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                                color: 'var(--text-heading)',
                                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                              }}
                                            />
                                          </div>
                                          <div>
                                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                              Description
                                            </label>
                                            <textarea
                                              value={section.description || ''}
                                              onChange={(e) => {
                                                e.stopPropagation();
                                                const updated = [...editingTemplateSections];
                                                updated[index].description = e.target.value;
                                                setEditingTemplateSections(updated);
                                              }}
                                              onFocus={(e) => e.stopPropagation()}
                                              onClick={(e) => e.stopPropagation()}
                                              rows={2}
                                              className="w-full px-2 py-1 rounded text-xs"
                                              style={{
                                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                                color: 'var(--text-heading)',
                                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                                resize: 'vertical',
                                              }}
                                            />
                                          </div>
                                          <div>
                                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                              Prompt Template
                                            </label>
                                            <textarea
                                              value={section.promptTemplate || section.prompt || ''}
                                              onChange={(e) => {
                                                e.stopPropagation();
                                                const updated = [...editingTemplateSections];
                                                updated[index].promptTemplate = e.target.value;
                                                setEditingTemplateSections(updated);
                                              }}
                                              onFocus={(e) => e.stopPropagation()}
                                              onClick={(e) => e.stopPropagation()}
                                              rows={3}
                                              className="w-full px-2 py-1 rounded text-xs"
                                              style={{
                                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                                color: 'var(--text-heading)',
                                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                                resize: 'vertical',
                                              }}
                                            />
                                          </div>
                                        </div>
                                      )}
                                    </div>
                                  );
                                })}
                              </div>
                            </div>
                          )}
                        </div>
                        <div className="mt-4 flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                          <button
                            type="button"
                            disabled={isSavingEditedTemplate}
                            onClick={async (e) => {
                              e.stopPropagation();
                              if (!editingTemplateName.trim() || editingTemplateSections.length === 0) {
                                alert('Template name and sections are required.');
                                return;
                              }
                              setIsSavingEditedTemplate(true);
                              try {
                                await api.updateArtifactTemplate(template.id, {
                                  name: editingTemplateName,
                                  description: editingTemplateDescription,
                                  defaultStructure: editingTemplateSections.map((section: any) => ({
                                    id: section.id,
                                    title: section.title,
                                    order: section.order,
                                    description: section.description || '',
                                    promptTemplate: section.promptTemplate || section.prompt || '',
                                    required: section.required !== undefined ? section.required : true,
                                    editable: section.editable !== undefined ? section.editable : true,
                                  })),
                                });
                                
                                // Reload templates
                                const templatesList = await api.listArtifactTemplates();
                                setTemplates(templatesList);
                                
                                // Reset editing state
                                setEditingTemplateId(null);
                                setEditingTemplateName('');
                                setEditingTemplateDescription('');
                                setEditingTemplateSections([]);
                                
                                alert('Template updated successfully!');
                              } catch (error: any) {
                                console.error('Error updating template:', error);
                                alert(`Failed to update template: ${error.message || 'Unknown error'}`);
                              } finally {
                                setIsSavingEditedTemplate(false);
                              }
                            }}
                            className="inline-flex items-center justify-center gap-2 px-4 py-2 rounded text-xs font-medium"
                            style={{
                              backgroundColor: 'var(--accent-primary)',
                              color: 'white',
                              border: 'none',
                              cursor: isSavingEditedTemplate ? 'not-allowed' : 'pointer',
                              opacity: isSavingEditedTemplate ? 0.6 : 1,
                            }}
                          >
                            {isSavingEditedTemplate ? 'Saving...' : t('artifacts.saveTemplate', { defaultValue: 'Save Template' })}
                          </button>
                          <button
                            type="button"
                            disabled={isSavingEditedTemplate}
                            onClick={(e) => {
                              e.stopPropagation();
                              setEditingTemplateId(null);
                              setEditingTemplateName('');
                              setEditingTemplateDescription('');
                              setEditingTemplateSections([]);
                            }}
                            className="inline-flex items-center justify-center gap-2 px-4 py-2 rounded text-xs font-medium"
                            style={{
                              backgroundColor: 'transparent',
                              color: 'var(--text-secondary-strong)',
                              border: '1px solid var(--overlay-line-90)',
                              cursor: isSavingEditedTemplate ? 'not-allowed' : 'pointer',
                            }}
                            onMouseEnter={(e) => {
                              if (!isSavingEditedTemplate) {
                                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                              }
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                          >
                            {t('artifacts.cancel', { defaultValue: 'Cancel' })}
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    );
  };
  
  // Separate EditTemplateForm component to prevent re-renders
  const EditTemplateForm = React.memo(({ 
    template,
    onSave,
    onCancel,
  }: {
    template: ArtifactTemplate;
    onSave: (name: string, description: string, sections: any[]) => Promise<void>;
    onCancel: () => void;
  }) => {
    const { t } = useTranslation();
    const [localName, setLocalName] = React.useState(template.name);
    const [localDescription, setLocalDescription] = React.useState(template.description || '');
    const [localSections, setLocalSections] = React.useState([...(template.defaultStructure || [])].sort((a, b) => a.order - b.order));
    const [saving, setSaving] = React.useState(false);
    const [localExpandedSections, setLocalExpandedSections] = React.useState<Record<string, boolean>>({});
    
    return (
      <div 
        className="mt-3 pt-3 pb-3 border-t border-b" 
        style={{ borderColor: 'var(--overlay-line-90)' }}
        onClick={(e) => e.stopPropagation()}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="space-y-4">
          {/* Template Name */}
          <div>
            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
              Template Name
            </label>
            <input
              type="text"
              value={localName}
              onChange={(e) => setLocalName(e.target.value)}
              onFocus={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
              className="w-full px-2 py-1.5 rounded text-xs"
              style={{
                backgroundColor: 'var(--bg-input, #1A1A1A)',
                color: 'var(--text-heading)',
                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
              }}
            />
          </div>
          
          {/* Template Description */}
          <div>
            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
              Description
            </label>
            <textarea
              value={localDescription}
              onChange={(e) => setLocalDescription(e.target.value)}
              onFocus={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
              rows={2}
              className="w-full px-2 py-1.5 rounded text-xs"
              style={{
                backgroundColor: 'var(--bg-input, #1A1A1A)',
                color: 'var(--text-heading)',
                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                resize: 'vertical',
              }}
            />
          </div>
          
          {/* Sections */}
          {localSections.length > 0 && (
            <div>
              <label className="block text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
                Sections
              </label>
              <div className="space-y-2">
                {localSections.map((section: any, index: number) => {
                  const isExpanded = localExpandedSections[`edit-${section.id}`] || false;
                  return (
                    <div
                      key={section.id}
                      className="px-3 py-2 rounded group"
                      style={{
                        backgroundColor: 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                      }}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <button
                          type="button"
                          onClick={() => {
                            setLocalExpandedSections((prev) => ({
                              ...prev,
                              [`edit-${section.id}`]: !prev[`edit-${section.id}`],
                            }));
                          }}
                          className="flex-1 flex items-center justify-between text-left mb-2"
                        >
                          <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
                            {section.title}
                            {section.required && (
                              <span className="ml-1 text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                                (required)
                              </span>
                            )}
                          </div>
                          <svg
                            className="w-3 h-3 transition-transform flex-shrink-0"
                            style={{
                              transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                              color: 'var(--text-secondary)',
                            }}
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                          >
                            <path d="M6 9l6 6 6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        </button>
                        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                          <button
                            type="button"
                            onClick={() => {
                              if (confirm(t('artifacts.confirmDeleteSection', { name: section.title, defaultValue: `Are you sure you want to delete section "${section.title}"?` }))) {
                                const updated = localSections.filter((s: any) => s.id !== section.id);
                                setLocalSections(updated);
                              }
                            }}
                            className="p-1 rounded"
                            title={t('artifacts.deleteSection', { defaultValue: 'Delete section' })}
                            style={{ backgroundColor: 'transparent' }}
                            onMouseEnter={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                            }}
                            onMouseLeave={(e) => {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }}
                          >
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                              />
                            </svg>
                          </button>
                        </div>
                      </div>
                      {isExpanded && (
                        <div 
                          className="space-y-2 mt-2 pt-2 border-t" 
                          style={{ borderColor: 'var(--overlay-line-90)' }}
                          onClick={(e) => e.stopPropagation()}
                          onMouseDown={(e) => e.stopPropagation()}
                        >
                          <div>
                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                              Title
                            </label>
                            <input
                              type="text"
                              value={section.title}
                              onChange={(e) => {
                                const updated = [...localSections];
                                updated[index].title = e.target.value;
                                setLocalSections(updated);
                              }}
                              onFocus={(e) => e.stopPropagation()}
                              onClick={(e) => e.stopPropagation()}
                              className="w-full px-2 py-1 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                              }}
                            />
                          </div>
                          <div>
                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                              Description
                            </label>
                            <textarea
                              value={section.description || ''}
                              onChange={(e) => {
                                const updated = [...localSections];
                                updated[index].description = e.target.value;
                                setLocalSections(updated);
                              }}
                              onFocus={(e) => e.stopPropagation()}
                              onClick={(e) => e.stopPropagation()}
                              rows={2}
                              className="w-full px-2 py-1 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                resize: 'vertical',
                              }}
                            />
                          </div>
                          <div>
                            <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                              Prompt Template
                            </label>
                            <textarea
                              value={section.promptTemplate || section.prompt || ''}
                              onChange={(e) => {
                                const updated = [...localSections];
                                updated[index].promptTemplate = e.target.value;
                                setLocalSections(updated);
                              }}
                              onFocus={(e) => e.stopPropagation()}
                              onClick={(e) => e.stopPropagation()}
                              rows={3}
                              className="w-full px-2 py-1 rounded text-xs"
                              style={{
                                backgroundColor: 'var(--bg-input, #1A1A1A)',
                                color: 'var(--text-heading)',
                                border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                resize: 'vertical',
                              }}
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
        <div className="mt-4 flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            disabled={saving}
            onClick={async (e) => {
              e.stopPropagation();
              if (!localName.trim() || localSections.length === 0) {
                alert('Template name and sections are required.');
                return;
              }
              setSaving(true);
              try {
                await onSave(localName, localDescription, localSections);
              } finally {
                setSaving(false);
              }
            }}
            className="inline-flex items-center justify-center gap-2 px-4 py-2 rounded text-xs font-medium"
            style={{
              backgroundColor: 'var(--accent-primary)',
              color: 'white',
              border: 'none',
              cursor: saving ? 'not-allowed' : 'pointer',
              opacity: saving ? 0.6 : 1,
            }}
          >
            {saving ? 'Saving...' : t('artifacts.saveTemplate', { defaultValue: 'Save Template' })}
          </button>
          <button
            type="button"
            disabled={saving}
            onClick={(e) => {
              e.stopPropagation();
              onCancel();
            }}
            className="inline-flex items-center justify-center gap-2 px-4 py-2 rounded text-xs font-medium"
            style={{
              backgroundColor: 'transparent',
              color: 'var(--text-secondary-strong)',
              border: '1px solid var(--overlay-line-90)',
              cursor: saving ? 'not-allowed' : 'pointer',
            }}
            onMouseEnter={(e) => {
              if (!saving) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
            }}
          >
            {t('artifacts.cancel', { defaultValue: 'Cancel' })}
          </button>
        </div>
      </div>
    );
  });

  // Wizard view - configure artifact from template
  if (wizardStep === 'configure' && selectedTemplate) {
    const sortedSections = [...(selectedTemplate.defaultStructure || [])].sort(
      (a, b) => a.order - b.order
    );

    if (isMobile) {
      // Mobile: templates list on top, configuration form below
      return (
        <div className="flex flex-col h-full" style={{ minHeight: 0 }}>
          {/* Back button - outside scrollable area */}
          <div className="pb-2">
            <button
              onClick={handleBack}
              className="flex items-center gap-2 text-xs"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              <svg
                className="w-4 h-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M15 19l-7-7 7-7"
                />
              </svg>
              Back
            </button>
          </div>

          <div className="flex-1 overflow-y-auto space-y-4 pt-2">
            {/* Templates list - mobile: on top */}
            <div>
              <div className="mb-2">
                <h4
                  className="text-[11px] uppercase tracking-wide"
                  style={{ color: 'var(--text-tertiary-weak)' }}
                >
                  Templates
                </h4>
              </div>
              <TemplatesList
                selectedTemplateId={selectedTemplate.id}
                onTemplateSelect={handleTemplateSelect}
                templates={templates}
                groupedTemplates={[...groupedSystemTemplates, ...groupedCustomTemplates]}
              />
            </div>

            {/* Configuration form */}
            <div className="pt-4 border-t" style={{ borderColor: 'var(--overlay-line-90)' }}>
              {/* Welcome message */}
              <WelcomeMessage template={selectedTemplate} />

        {/* Artifact name input */}
        <div>
          <label
            className="block text-13 mb-2"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            Content name
          </label>
          <input
            type="text"
            value={artifactName}
            onChange={(e) => setArtifactName(e.target.value)}
            placeholder="Enter content name"
            className="w-full text-xs"
            style={{
              backgroundColor: 'transparent',
              border: 'none',
              outline: 'none',
              color: 'var(--text-heading)',
              padding: '8px 0',
              borderBottom: '1px solid var(--overlay-line-90)',
            }}
            onMouseEnter={(e) => {
              const el = e.currentTarget as HTMLInputElement;
              el.style.backgroundColor = 'var(--white-06)';
              el.style.borderRadius = '6px';
              el.style.padding = '8px 8px';
            }}
            onMouseLeave={(e) => {
              const el = e.currentTarget as HTMLInputElement;
              el.style.backgroundColor = 'transparent';
              el.style.borderRadius = '0';
              el.style.padding = '8px 0';
            }}
          />
        </div>

        {/* Document models */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label
              className="block text-13 mb-2"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Orchestrator model
            </label>
            <select
              value={orchestratorModel}
              onChange={(e) => setOrchestratorModel(e.target.value)}
              className="w-full text-xs px-2 py-1.5 rounded"
              style={{
                backgroundColor: 'var(--white-06)',
                border: '1px solid var(--overlay-line-90)',
                color: 'var(--text-heading)',
                outline: 'none',
              }}
            >
              <option value="gemini_fast">Gemini Fast</option>
              <option value="gemini_balanced">Gemini Balanced</option>
              <option value="gemini_quality">Gemini Quality</option>
              <option value="gpt_fast">GPT Fast</option>
              <option value="gpt_balanced">GPT Balanced</option>
              <option value="gpt_quality">GPT Quality</option>
              <option value="grok_fast">Grok Fast</option>
              <option value="grok_balanced">Grok Balanced</option>
              <option value="grok_quality">Grok Quality</option>
              <option value="claude_fast">Claude Fast</option>
              <option value="claude_balanced">Claude Balanced</option>
              <option value="deepseek_fast">DeepSeek Fast</option>
              <option value="deepseek_quality">DeepSeek Quality</option>
            </select>
          </div>
          <div>
            <label
              className="block text-13 mb-2"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Default section model
            </label>
            <select
              value={defaultModel}
              onChange={(e) => {
                const newDefault = e.target.value;
                const oldDefault = defaultModel;
                setDefaultModel(newDefault);
                // Update all sections that use the default model
                setSectionModels((prev) => {
                  const updated = { ...prev };
                  // Update all sections from template that use default
                  if (selectedTemplate?.defaultStructure) {
                    selectedTemplate.defaultStructure.forEach((section: { id: string }) => {
                      const currentModel = prev[section.id];
                      // If section doesn't have individual setting OR uses old default, update to new default
                      if (!currentModel || currentModel === oldDefault) {
                        updated[section.id] = newDefault;
                      }
                      // Keep individual settings that differ from old default unchanged
                    });
                  }
                  return updated;
                });
              }}
              className="w-full text-xs px-2 py-1.5 rounded"
              style={{
                backgroundColor: 'var(--white-06)',
                border: '1px solid var(--overlay-line-90)',
                color: 'var(--text-heading)',
                outline: 'none',
              }}
            >
              <option value="gemini_fast">Gemini Fast</option>
              <option value="gemini_balanced">Gemini Balanced</option>
              <option value="gemini_quality">Gemini Quality</option>
              <option value="gpt_fast">GPT Fast</option>
              <option value="gpt_balanced">GPT Balanced</option>
              <option value="gpt_quality">GPT Quality</option>
              <option value="grok_fast">Grok Fast</option>
              <option value="grok_balanced">Grok Balanced</option>
              <option value="grok_quality">Grok Quality</option>
              <option value="claude_fast">Claude Fast</option>
              <option value="claude_balanced">Claude Balanced</option>
              <option value="deepseek_fast">DeepSeek Fast</option>
              <option value="deepseek_quality">DeepSeek Quality</option>
            </select>
          </div>
        </div>

        {/* Sections list */}
        {sortedSections.length > 0 && (
          <div>
            <label
              className="block text-13 mb-2"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Sections
            </label>
            <div className="space-y-2">
              {sortedSections.map((section) => {
                const isExpanded = expandedSections[section.id] || false;
                return (
                <div
                  key={section.id}
                  className="px-3 py-2 rounded"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                >
                  <button
                    type="button"
                    onClick={() => {
                      setExpandedSections((prev) => ({
                        ...prev,
                        [section.id]: !prev[section.id],
                      }));
                    }}
                    className="w-full flex items-center justify-between text-left"
                  >
                    <div
                      className="text-xs font-medium"
                      style={{ color: 'var(--text-heading)' }}
                    >
                      {section.title}
                      {section.required && (
                        <span
                          className="ml-1 text-[10px]"
                          style={{ color: 'var(--text-tertiary-weak)' }}
                        >
                          (required)
                        </span>
                      )}
                    </div>
                    <svg
                      className="w-3 h-3 transition-transform"
                      style={{
                        transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                        color: 'var(--text-secondary)',
                      }}
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                    >
                      <path
                        d="M6 9l6 6 6-6"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                    </svg>
                  </button>
                  {isExpanded && (
                    <>
                      {section.description && (
                        <div
                          className="text-[10px] mb-1 mt-2"
                          style={{ color: 'var(--text-secondary-strong)' }}
                        >
                          {section.description}
                        </div>
                      )}
                      {section.promptTemplate && (
                        <div
                          className="text-[10px] mt-1 pt-1"
                          style={{
                            color: 'var(--text-tertiary-weak)',
                            borderTop: '1px solid var(--overlay-line-90)',
                            fontStyle: 'italic',
                          }}
                        >
                          {section.promptTemplate}
                        </div>
                      )}
                      <div
                        className="mt-2 pt-2 space-y-2"
                        style={{
                          borderTop: '1px solid var(--overlay-line-90)',
                        }}
                      >
                        <div>
                          <label
                            className="block text-[10px] mb-1"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Model
                          </label>
                          <select
                            value={sectionModels[section.id] || defaultModel}
                            onChange={(e) => {
                              setSectionModels((prev) => ({
                                ...prev,
                                [section.id]: e.target.value,
                              }));
                            }}
                            className="w-full text-[10px] px-2 py-1 rounded"
                            style={{
                              backgroundColor: 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                              outline: 'none',
                            }}
                          >
                            <option value="gemini_fast">Gemini Fast</option>
                            <option value="gemini_balanced">Gemini Balanced</option>
                            <option value="gemini_quality">Gemini Quality</option>
                            <option value="gpt_fast">GPT Fast</option>
                            <option value="gpt_balanced">GPT Balanced</option>
                            <option value="gpt_quality">GPT Quality</option>
                            <option value="grok_fast">Grok Fast</option>
                            <option value="grok_balanced">Grok Balanced</option>
                            <option value="grok_quality">Grok Quality</option>
                            <option value="claude_fast">Claude Fast</option>
                            <option value="claude_balanced">Claude Balanced</option>
                            <option value="deepseek_fast">DeepSeek Fast</option>
                            <option value="deepseek_quality">DeepSeek Quality</option>
                          </select>
                        </div>
                        <label className="flex items-center gap-1.5 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={sectionAutoUpdate[section.id] || false}
                            onChange={(e) => {
                              setSectionAutoUpdate((prev) => ({
                                ...prev,
                                [section.id]: e.target.checked,
                              }));
                            }}
                            className="w-3 h-3"
                            style={{
                              accentColor: 'var(--accent-primary)',
                            }}
                          />
                          <span
                            className="text-[10px]"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Auto-update
                          </span>
                        </label>
                      </div>
                    </>
                  )}
                </div>
                );
              })}
            </div>
          </div>
        )}

        {/* User request field */}
        <div className="pt-4">
          <label
            className="block text-13 mb-2"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            Document request (optional)
          </label>
          <textarea
            value={userRequest}
            onChange={(e) => setUserRequest(e.target.value)}
            placeholder="Describe what this document should cover or focus on..."
            rows={3}
            className="w-full text-xs resize-none"
            style={{
              backgroundColor: 'var(--bg-input, transparent)',
              border: '1px solid var(--border-input, var(--overlay-line-90))',
              borderRadius: '8px',
              padding: '10px 12px',
              outline: 'none',
              color: 'var(--text-heading)',
              transition: 'all 0.2s ease',
            }}
            onFocus={(e) => {
              e.currentTarget.style.borderColor = 'var(--text-secondary)';
              e.currentTarget.style.backgroundColor = 'var(--white-06)';
            }}
            onBlur={(e) => {
              e.currentTarget.style.borderColor = 'var(--overlay-line-90)';
              e.currentTarget.style.backgroundColor = 'var(--bg-input, transparent)';
            }}
          />
        </div>

        {/* Document auto-update settings */}
        <div className="pt-4">
          <label className="flex items-center gap-2 cursor-pointer mb-2">
            <input
              type="checkbox"
              checked={documentAutoUpdateEnabled}
              onChange={(e) => setDocumentAutoUpdateEnabled(e.target.checked)}
              className="w-3 h-3"
              style={{
                accentColor: 'var(--accent-primary)',
              }}
            />
            <span
              className="text-13"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Enable document auto-update
            </span>
          </label>
          {documentAutoUpdateEnabled && (
            <div className="ml-5 mt-3 space-y-3">
              <div>
                <label
                  className="block text-[11px] mb-1.5"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Trigger
                </label>
                <select
                  value={autoUpdateTrigger}
                  onChange={(e) => setAutoUpdateTrigger(e.target.value as any)}
                  className="w-full text-xs px-2 py-1.5 rounded"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    border: '1px solid var(--overlay-line-90)',
                    color: 'var(--text-heading)',
                    outline: 'none',
                  }}
                >
                  <option value="on_new_message">On new message</option>
                  <option value="time_based">Time-based</option>
                  <option value="on_new_document">On new document</option>
                </select>
              </div>

              {autoUpdateTrigger === 'time_based' && (
                <div className="space-y-3">
                  <div>
                    <label
                      className="block text-[11px] mb-1.5"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Schedule Type
                    </label>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        onClick={() => setAutoUpdateScheduleType('daily')}
                        className="text-xs px-3 py-1.5 rounded transition-colors"
                        style={{
                          backgroundColor: autoUpdateScheduleType === 'daily' 
                            ? 'var(--white-10)' 
                            : 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                        }}
                      >
                        Daily
                      </button>
                      <button
                        type="button"
                        onClick={() => setAutoUpdateScheduleType('weekly')}
                        className="text-xs px-3 py-1.5 rounded transition-colors"
                        style={{
                          backgroundColor: autoUpdateScheduleType === 'weekly' 
                            ? 'var(--white-10)' 
                            : 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                        }}
                      >
                        Weekly
                      </button>
                    </div>
                  </div>
                  
                <div>
                  <label
                    className="block text-[11px] mb-1.5"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    Schedule Time (UTC)
                  </label>
                  <div className="w-fit">
                    <input
                      type="time"
                      value={autoUpdateScheduleTime}
                      onChange={(e) => setAutoUpdateScheduleTime(e.target.value)}
                      className="text-xs px-2 py-1.5 rounded"
                      style={{
                        backgroundColor: 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                        color: 'var(--text-heading)',
                        outline: 'none',
                        width: 'auto',
                      }}
                    />
                  </div>
                  <p className="text-[10px] mt-1" style={{ color: 'var(--text-secondary)' }}>
                      {autoUpdateScheduleType === 'daily' 
                        ? 'Time when the update should run each day (24-hour format)' 
                        : 'Time when the update should run each week (24-hour format)'}
                    </p>
                  </div>
                  
                    <button
                      type="button"
                      onClick={() => {
                        setShowContextTriggers((prev) => {
                          const next = !prev;
                          if (!next) {
                            setCollectedSignals(null);
                          }
                          return next;
                        });
                      }}
                      className="w-full text-xs px-3 py-1.5 rounded transition-colors text-left"
                      style={{
                        backgroundColor: showContextTriggers 
                          ? 'var(--white-10)' 
                          : 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                        color: 'var(--text-heading)',
                      }}
                    >
                      Context update signals
                    </button>
                  </div>
                )}

              <div>
                <button
                  type="button"
                  onClick={() => setShowConditions(!showConditions)}
                  className="flex items-center gap-2 mb-1.5 cursor-pointer"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  <svg 
                    className="w-3 h-3 transition-transform"
                    style={{ transform: showConditions ? 'rotate(90deg)' : 'rotate(0deg)' }}
                    viewBox="0 0 24 24" 
                    fill="none" 
                    stroke="currentColor" 
                    strokeWidth="2" 
                    strokeLinecap="round" 
                    strokeLinejoin="round"
                  >
                    <path d="M9 18l6-6-6-6"/>
                  </svg>
                  <span className="text-[11px]">Conditions</span>
                </button>
                {showConditions && (
                <div className="space-y-2">
                  <div className="grid grid-cols-2 gap-2">
                    <div>
                      <label
                        className="block text-[10px] mb-1"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Min new messages
                      </label>
                      <input
                        type="number"
                        min="0"
                        value={autoUpdateMinNewMessages || ''}
                        onChange={(e) => setAutoUpdateMinNewMessages(e.target.value ? parseInt(e.target.value) : undefined)}
                        placeholder="Optional"
                        className="w-full text-xs px-2 py-1 rounded"
                        style={{
                          backgroundColor: 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                          outline: 'none',
                        }}
                      />
                    </div>
                    <div>
                      <label
                        className="block text-[10px] mb-1"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Min context change (0-1)
                      </label>
                      <input
                        type="number"
                        min="0"
                        max="1"
                        step="0.1"
                        value={autoUpdateMinContextChange || ''}
                        onChange={(e) => setAutoUpdateMinContextChange(e.target.value ? parseFloat(e.target.value) : undefined)}
                        placeholder="Optional"
                        className="w-full text-xs px-2 py-1 rounded"
                        style={{
                          backgroundColor: 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                          outline: 'none',
                        }}
                      />
                    </div>
                  </div>
                  <div>
                    <label
                      className="block text-[10px] mb-1"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Keywords (comma-separated)
                    </label>
                    <input
                      type="text"
                      value={autoUpdateKeywords}
                      onChange={(e) => setAutoUpdateKeywords(e.target.value)}
                      placeholder="Optional"
                      className="w-full text-xs px-2 py-1 rounded"
                      style={{
                        backgroundColor: 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                        color: 'var(--text-heading)',
                        outline: 'none',
                      }}
                    />
                  </div>
                  <label className="flex items-center gap-1.5 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={autoUpdateSearchQueryChanged}
                      onChange={(e) => setAutoUpdateSearchQueryChanged(e.target.checked)}
                      className="w-3 h-3"
                      style={{
                        accentColor: 'var(--accent-primary)',
                      }}
                    />
                    <span
                      className="text-[10px]"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Trigger if search query changed
                    </span>
                  </label>
                </div>
                )}
              </div>

              <div>
                <label className="flex items-center gap-2 cursor-pointer mb-2">
                  <input
                    type="checkbox"
                    checked={autoUpdateCheckQuality}
                    onChange={(e) => setAutoUpdateCheckQuality(e.target.checked)}
                    className="w-3 h-3"
                    style={{
                      accentColor: 'var(--accent-primary)',
                    }}
                  />
                  <span
                    className="text-[11px]"
                    style={{ color: 'var(--text-secondary-strong)' }}
                  >
                    Smart update (check quality)
                  </span>
                </label>
                {autoUpdateCheckQuality && (
                  <div className="ml-5 mt-2 space-y-2">
                    <div>
                      <label
                        className="block text-[10px] mb-1"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Min quality threshold (0-1)
                      </label>
                      <input
                        type="number"
                        min="0"
                        max="1"
                        step="0.1"
                        value={autoUpdateMinQualityThreshold}
                        onChange={(e) => setAutoUpdateMinQualityThreshold(parseFloat(e.target.value))}
                        className="w-full text-xs px-2 py-1 rounded"
                        style={{
                          backgroundColor: 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                          outline: 'none',
                        }}
                      />
                    </div>
                    <label className="flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={autoUpdateNotifyOnUpdate}
                        onChange={(e) => setAutoUpdateNotifyOnUpdate(e.target.checked)}
                        className="w-3 h-3"
                        style={{
                          accentColor: 'var(--accent-primary)',
                        }}
                      />
                      <span
                        className="text-[10px]"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Notify on update
                      </span>
                    </label>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>

        {/* Generate button */}
        <div className="pt-2 flex justify-end">
          <button
            onClick={handleGeneratePreview}
            disabled={!artifactName.trim() || generating}
            className="text-xs px-3 py-1.5 rounded transition-colors"
            style={{
              backgroundColor: artifactName.trim() && !generating
                ? 'var(--white-10)'
                : 'var(--white-06)',
              color: artifactName.trim() && !generating
                ? 'var(--text-heading)'
                : 'var(--text-tertiary-weak)',
              border: '1px solid var(--overlay-line-90)',
              cursor: artifactName.trim() && !generating ? 'pointer' : 'not-allowed',
            }}
            onMouseEnter={(e) => {
              if (artifactName.trim() && !generating) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
              }
            }}
            onMouseLeave={(e) => {
              if (artifactName.trim() && !generating) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
          >
            {generating 
              ? `Generating... (${generationProgress.current}/${generationProgress.total})`
              : 'Next'}
          </button>
        </div>
            </div>
          </div>
        </div>
      );
    }

    // Desktop: templates list on left sidebar, configuration form on right
    return (
      <div className="flex flex-row h-full gap-4" style={{ minHeight: 0 }}>
        {/* Left sidebar: Templates list */}
        <div 
          className="flex-shrink-0 overflow-y-auto"
          style={{ 
            width: '200px',
            borderRight: '1px solid var(--overlay-line-90)',
            paddingRight: '12px',
          }}
        >
          <div className="pb-2 mb-2 border-b" style={{ borderColor: 'var(--overlay-line-90)' }}>
            <button
              onClick={handleBack}
              className="flex items-center gap-2 text-xs w-full"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              <svg
                className="w-4 h-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M15 19l-7-7 7-7"
                />
              </svg>
              Back
            </button>
          </div>
          <TemplatesList
            compact={true}
            selectedTemplateId={selectedTemplate.id}
            onTemplateSelect={handleTemplateSelect}
            templates={templates}
            groupedTemplates={[...groupedSystemTemplates, ...groupedCustomTemplates]}
          />
        </div>

        {/* Right side: Configuration form */}
        <div className="flex-1 flex flex-col overflow-hidden">
          <div className="flex-1 overflow-y-auto space-y-4">
            {/* Welcome message */}
            <WelcomeMessage template={selectedTemplate} />

            {/* Artifact name input */}
            <div>
              <label
                className="block text-13 mb-2"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                Content name
              </label>
              <input
                type="text"
                value={artifactName}
                onChange={(e) => setArtifactName(e.target.value)}
                placeholder="Enter content name"
                className="w-full text-xs"
                style={{
                  backgroundColor: 'transparent',
                  border: 'none',
                  outline: 'none',
                  color: 'var(--text-heading)',
                  padding: '8px 0',
                  borderBottom: '1px solid var(--overlay-line-90)',
                }}
                onMouseEnter={(e) => {
                  const el = e.currentTarget as HTMLInputElement;
                  el.style.backgroundColor = 'var(--white-06)';
                  el.style.borderRadius = '6px';
                  el.style.padding = '8px 8px';
                }}
                onMouseLeave={(e) => {
                  const el = e.currentTarget as HTMLInputElement;
                  el.style.backgroundColor = 'transparent';
                  el.style.borderRadius = '0';
                  el.style.padding = '8px 0';
                }}
              />
            </div>

            {/* Document models */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label
                  className="block text-13 mb-2"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Orchestrator model
                </label>
                <select
                  value={orchestratorModel}
                  onChange={(e) => setOrchestratorModel(e.target.value)}
                  className="w-full text-xs px-2 py-1.5 rounded"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    border: '1px solid var(--overlay-line-90)',
                    color: 'var(--text-heading)',
                    outline: 'none',
                  }}
                >
                  <option value="gemini_fast">Gemini Fast</option>
                  <option value="gemini_balanced">Gemini Balanced</option>
                  <option value="gemini_quality">Gemini Quality</option>
                  <option value="gpt_fast">GPT Fast</option>
                  <option value="gpt_balanced">GPT Balanced</option>
                  <option value="gpt_quality">GPT Quality</option>
                  <option value="grok_fast">Grok Fast</option>
                  <option value="grok_balanced">Grok Balanced</option>
                  <option value="grok_quality">Grok Quality</option>
                  <option value="claude_fast">Claude Fast</option>
                  <option value="claude_balanced">Claude Balanced</option>
                  <option value="deepseek_fast">DeepSeek Fast</option>
                  <option value="deepseek_quality">DeepSeek Quality</option>
                </select>
              </div>
              <div>
                <label
                  className="block text-13 mb-2"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Default section model
                </label>
                <select
                  value={defaultModel}
                  onChange={(e) => {
                    const newDefault = e.target.value;
                    const oldDefault = defaultModel;
                    setDefaultModel(newDefault);
                    // Update all sections that use the default model
                    setSectionModels((prev) => {
                      const updated = { ...prev };
                      // Update all sections from template that use default
                      if (selectedTemplate?.defaultStructure) {
                        selectedTemplate.defaultStructure.forEach((section: { id: string }) => {
                          const currentModel = prev[section.id];
                          // If section doesn't have individual setting OR uses old default, update to new default
                          if (!currentModel || currentModel === oldDefault) {
                            updated[section.id] = newDefault;
                          }
                          // Keep individual settings that differ from old default unchanged
                        });
                      }
                      return updated;
                    });
                  }}
                  className="w-full text-xs px-2 py-1.5 rounded"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    border: '1px solid var(--overlay-line-90)',
                    color: 'var(--text-heading)',
                    outline: 'none',
                  }}
                >
                  <option value="gemini_fast">Gemini Fast</option>
                  <option value="gemini_balanced">Gemini Balanced</option>
                  <option value="gemini_quality">Gemini Quality</option>
                  <option value="gpt_fast">GPT Fast</option>
                  <option value="gpt_balanced">GPT Balanced</option>
                  <option value="gpt_quality">GPT Quality</option>
                  <option value="grok_fast">Grok Fast</option>
                  <option value="grok_balanced">Grok Balanced</option>
                  <option value="grok_quality">Grok Quality</option>
                  <option value="claude_fast">Claude Fast</option>
                  <option value="claude_balanced">Claude Balanced</option>
                  <option value="deepseek_fast">DeepSeek Fast</option>
                  <option value="deepseek_quality">DeepSeek Quality</option>
                </select>
              </div>
            </div>

            {/* Sections list */}
            {sortedSections.length > 0 && (
              <div>
                <label
                  className="block text-13 mb-2"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Sections
                </label>
                <div className="space-y-2">
                  {sortedSections.map((section) => {
                    const isExpanded = expandedSections[section.id] || false;
                    return (
                    <div
                      key={section.id}
                      className="px-3 py-2 rounded"
                      style={{
                        backgroundColor: 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                      }}
                    >
                      <button
                        type="button"
                        onClick={() => {
                          setExpandedSections((prev) => ({
                            ...prev,
                            [section.id]: !prev[section.id],
                          }));
                        }}
                        className="w-full flex items-center justify-between text-left"
                      >
                        <div
                          className="text-xs font-medium"
                          style={{ color: 'var(--text-heading)' }}
                        >
                          {section.title}
                          {section.required && (
                            <span
                              className="ml-1 text-[10px]"
                              style={{ color: 'var(--text-tertiary-weak)' }}
                            >
                              (required)
                            </span>
                          )}
                        </div>
                        <svg
                          className="w-3 h-3 transition-transform"
                          style={{
                            transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)',
                            color: 'var(--text-secondary)',
                          }}
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                        >
                          <path
                            d="M6 9l6 6 6-6"
                            strokeWidth="2"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          />
                        </svg>
                      </button>
                      {isExpanded && (
                        <>
                          {section.description && (
                            <div
                              className="text-[10px] mb-1 mt-2"
                              style={{ color: 'var(--text-secondary-strong)' }}
                            >
                              {section.description}
                            </div>
                          )}
                          {section.promptTemplate && (
                            <div
                              className="text-[10px] mt-1 pt-1"
                              style={{
                                color: 'var(--text-tertiary-weak)',
                                borderTop: '1px solid var(--overlay-line-90)',
                                fontStyle: 'italic',
                              }}
                            >
                              {section.promptTemplate}
                            </div>
                          )}
                          <div
                            className="mt-2 pt-2 space-y-2"
                            style={{
                              borderTop: '1px solid var(--overlay-line-90)',
                            }}
                          >
                            <div>
                              <label
                                className="block text-[10px] mb-1"
                                style={{ color: 'var(--text-secondary-strong)' }}
                              >
                                Model
                              </label>
                              <select
                                value={sectionModels[section.id] || defaultModel}
                                onChange={(e) => {
                                  setSectionModels((prev) => ({
                                    ...prev,
                                    [section.id]: e.target.value,
                                  }));
                                }}
                                className="w-full text-[10px] px-2 py-1 rounded"
                                style={{
                                  backgroundColor: 'var(--white-06)',
                                  border: '1px solid var(--overlay-line-90)',
                                  color: 'var(--text-heading)',
                                  outline: 'none',
                                }}
                              >
                                <option value="gemini_fast">Gemini Fast</option>
                                <option value="gemini_balanced">Gemini Balanced</option>
                                <option value="gemini_quality">Gemini Quality</option>
                                <option value="gpt_fast">GPT Fast</option>
                                <option value="gpt_balanced">GPT Balanced</option>
                                <option value="gpt_quality">GPT Quality</option>
                                <option value="grok_fast">Grok Fast</option>
                                <option value="grok_balanced">Grok Balanced</option>
                                <option value="grok_quality">Grok Quality</option>
                                <option value="claude_fast">Claude Fast</option>
                                <option value="claude_balanced">Claude Balanced</option>
                                <option value="deepseek_fast">DeepSeek Fast</option>
                                <option value="deepseek_quality">DeepSeek Quality</option>
                              </select>
                            </div>
                            <label className="flex items-center gap-1.5 cursor-pointer">
                              <input
                                type="checkbox"
                                checked={sectionAutoUpdate[section.id] || false}
                                onChange={(e) => {
                                  setSectionAutoUpdate((prev) => ({
                                    ...prev,
                                    [section.id]: e.target.checked,
                                  }));
                                }}
                                className="w-3 h-3"
                                style={{
                                  accentColor: 'var(--accent-primary)',
                                }}
                              />
                              <span
                                className="text-[10px]"
                                style={{ color: 'var(--text-secondary-strong)' }}
                              >
                                Auto-update
                              </span>
                            </label>
                          </div>
                        </>
                      )}
                    </div>
                    );
                  })}
                </div>
              </div>
            )}

            {/* User request field */}
            <div className="pt-4">
              <label
                className="block text-13 mb-2"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                Document request (optional)
              </label>
              <textarea
                value={userRequest}
                onChange={(e) => setUserRequest(e.target.value)}
                placeholder="Describe what this document should cover or focus on..."
                rows={3}
                className="w-full text-xs resize-none"
                style={{
                  backgroundColor: 'var(--bg-input, transparent)',
                  border: '1px solid var(--border-input, var(--overlay-line-90))',
                  borderRadius: '8px',
                  padding: '10px 12px',
                  outline: 'none',
                  color: 'var(--text-heading)',
                  transition: 'all 0.2s ease',
                }}
                onFocus={(e) => {
                  e.currentTarget.style.borderColor = 'var(--text-secondary)';
                  e.currentTarget.style.backgroundColor = 'var(--white-06)';
                }}
                onBlur={(e) => {
                  e.currentTarget.style.borderColor = 'var(--overlay-line-90)';
                  e.currentTarget.style.backgroundColor = 'var(--bg-input, transparent)';
                }}
              />
            </div>

            {/* Document auto-update settings */}
            <div className="pt-4">
              <label className="flex items-center gap-2 cursor-pointer mb-2">
                <input
                  type="checkbox"
                  checked={documentAutoUpdateEnabled}
                  onChange={(e) => setDocumentAutoUpdateEnabled(e.target.checked)}
                  className="w-3 h-3"
                  style={{
                    accentColor: 'var(--accent-primary)',
                  }}
                />
                <span
                  className="text-13"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Enable document auto-update
                </span>
              </label>
              {documentAutoUpdateEnabled && (
                <div className="ml-5 mt-3 space-y-3">
                  <div>
                    <label
                      className="block text-[11px] mb-1.5"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Trigger
                    </label>
                    <select
                      value={autoUpdateTrigger}
                      onChange={(e) => setAutoUpdateTrigger(e.target.value as any)}
                      className="w-full text-xs px-2 py-1.5 rounded"
                      style={{
                        backgroundColor: 'var(--white-06)',
                        border: '1px solid var(--overlay-line-90)',
                        color: 'var(--text-heading)',
                        outline: 'none',
                      }}
                    >
                      <option value="on_new_message">On new message</option>
                      <option value="time_based">Time-based</option>
                      <option value="on_new_document">On new document</option>
                    </select>
                  </div>

                  {autoUpdateTrigger === 'time_based' && (
                    <div className="space-y-3">
                      <div>
                        <label
                          className="block text-[11px] mb-1.5"
                          style={{ color: 'var(--text-secondary-strong)' }}
                        >
                          Schedule Type
                        </label>
                        <div className="flex gap-2">
                          <button
                            type="button"
                            onClick={() => setAutoUpdateScheduleType('daily')}
                            className="flex-1 text-xs px-3 py-1.5 rounded transition-colors"
                            style={{
                              backgroundColor: autoUpdateScheduleType === 'daily' 
                                ? 'var(--white-10)' 
                                : 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                            }}
                          >
                            Daily
                          </button>
                          <button
                            type="button"
                            onClick={() => setAutoUpdateScheduleType('weekly')}
                            className="flex-1 text-xs px-3 py-1.5 rounded transition-colors"
                            style={{
                              backgroundColor: autoUpdateScheduleType === 'weekly' 
                                ? 'var(--white-10)' 
                                : 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                            }}
                          >
                            Weekly
                          </button>
                        </div>
                      </div>
                      
                    <div>
                      <label
                        className="block text-[11px] mb-1.5"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Schedule Time (UTC)
                      </label>
                      <div className="w-fit">
                        <input
                          type="time"
                          value={autoUpdateScheduleTime}
                          onChange={(e) => setAutoUpdateScheduleTime(e.target.value)}
                          className="text-xs px-2 py-1.5 rounded"
                          style={{
                            backgroundColor: 'var(--white-06)',
                            border: '1px solid var(--overlay-line-90)',
                            color: 'var(--text-heading)',
                            outline: 'none',
                            width: 'auto',
                          }}
                        />
                      </div>
                      <p className="text-[10px] mt-1" style={{ color: 'var(--text-secondary)' }}>
                          {autoUpdateScheduleType === 'daily' 
                            ? 'Time when the update should run each day (24-hour format)' 
                            : 'Time when the update should run each week (24-hour format)'}
                        </p>
                      </div>
                      
                      <button
                        type="button"
                        onClick={() => setShowContextTriggers(!showContextTriggers)}
                        className="w-full text-xs px-3 py-1.5 rounded transition-colors text-left"
                        style={{
                          backgroundColor: showContextTriggers 
                            ? 'var(--white-10)' 
                            : 'var(--white-06)',
                          border: '1px solid var(--overlay-line-90)',
                          color: 'var(--text-heading)',
                        }}
                      >
                        Context update signals
                      </button>
                    </div>
                  )}

                  {showContextTriggers && (
                    <div className="space-y-3">
                      <div style={{ borderTop: '1px solid var(--overlay-line-90)' }}></div>
                      
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={handleCollectAutomatically}
                          disabled={isCollectingSignals}
                          className="text-xs px-3 py-1.5 rounded transition-colors flex items-center gap-2"
                          style={{
                            backgroundColor: 'var(--white-06)',
                            border: '1px solid var(--overlay-line-90)',
                            color: 'var(--text-heading)',
                            opacity: isCollectingSignals ? 0.6 : 1,
                            cursor: isCollectingSignals ? 'not-allowed' : 'pointer',
                          }}
                        >
                          {isCollectingSignals ? (
                            <svg 
                              className="w-3.5 h-3.5 animate-spin" 
                              viewBox="0 0 24 24" 
                              fill="none" 
                              stroke="currentColor" 
                              strokeWidth="2" 
                              strokeLinecap="round" 
                              strokeLinejoin="round"
                            >
                              <path d="M21 12a9 9 0 11-6.219-8.56"/>
                            </svg>
                          ) : (
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0118.8-4.3M22 12.5a10 10 0 01-18.8 4.2"/>
                            </svg>
                          )}
                          <span>Collect automatically</span>
                        </button>
                        
                        <button
                          type="button"
                          className="text-xs px-3 py-1.5 rounded transition-colors flex items-center gap-2"
                          style={{
                            backgroundColor: 'var(--white-06)',
                            border: '1px solid var(--overlay-line-90)',
                            color: 'var(--text-heading)',
                          }}
                        >
                          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M12 5v14M5 12h14"/>
                          </svg>
                          <span>Add</span>
                        </button>
                      </div>
                      
                      {isCollectingSignals && (
                        <div className="text-xs py-2 space-y-1" style={{ color: 'var(--text-secondary-strong)' }}>
                          <div>Request is being processed. The system is identifying update signals.</div>
                          {averageTimingSeconds !== null && (
                            <div style={{ color: 'var(--text-secondary)', fontSize: '10px' }}>
                              Average response time: ~{averageTimingSeconds}s • Elapsed: {elapsedSeconds}s
                            </div>
                          )}
                        </div>
                      )}
                      
                      {collectedSignals && !isCollectingSignals && !collectedSignals.error && (
                        <div className="mt-3 space-y-2">
                          {collectedSignals.signals && Array.isArray(collectedSignals.signals) && collectedSignals.signals.length > 0 ? (
                            <div className="space-y-3">
                              <div className="text-[11px] font-medium" style={{ color: 'var(--text-secondary-strong)' }}>
                                Collected Signals ({collectedSignals.signals.length})
                              </div>
                              {collectedSignals.signals.map((signal: any, index: number) => (
                                <div 
                                  key={index}
                                  className="p-3 rounded"
                                  style={{ 
                                    backgroundColor: 'var(--white-06)', 
                                    border: '1px solid var(--overlay-line-90)' 
                                  }}
                                >
                                  <div className="space-y-2">
                                    <div>
                                      <div className="text-[11px] font-medium mb-1" style={{ color: 'var(--text-heading)' }}>
                                        {signal.name || `Signal ${index + 1}`}
                                      </div>
                                      {signal.description && (
                                        <div className="text-[10px] mb-2" style={{ color: 'var(--text-secondary)' }}>
                                          {signal.description}
                                        </div>
                                      )}
                                      <div className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>
                                        <span className="font-medium">Signal:</span> {signal.signal || 'N/A'}
                                      </div>
                                    </div>
                                    {signal.searchQueries && Array.isArray(signal.searchQueries) && signal.searchQueries.length > 0 && (
                                      <div>
                                        <div className="text-[10px] font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                          Search Queries:
                                        </div>
                                        <div className="flex flex-wrap gap-1">
                                          {signal.searchQueries.map((query: string, qIndex: number) => (
                                            <span
                                              key={qIndex}
                                              className="text-[9px] px-2 py-0.5 rounded"
                                              style={{
                                                backgroundColor: 'var(--white-10)',
                                                border: '1px solid var(--overlay-line-90)',
                                                color: 'var(--text-heading)',
                                              }}
                                            >
                                              {query}
                                            </span>
                                          ))}
                                        </div>
                                      </div>
                                    )}
                                  </div>
                                </div>
                              ))}
                            </div>
                          ) : (
                            <div className="text-[10px] py-2" style={{ color: 'var(--text-secondary)' }}>
                              No signals found in the discussion.
                            </div>
                          )}
                        </div>
                      )}
                      
                      {collectedSignals && !isCollectingSignals && collectedSignals.error && (
                        <div className="mt-2 p-2 rounded text-[10px]" style={{ backgroundColor: 'var(--white-06)', border: '1px solid var(--overlay-line-90)', color: 'var(--accent-danger)' }}>
                          Error: {collectedSignals.error}
                        </div>
                      )}
                      
                      <div style={{ borderBottom: '1px solid var(--overlay-line-90)' }}></div>
                    </div>
                  )}

                  <div>
                    <button
                      type="button"
                      onClick={() => setShowConditions(!showConditions)}
                      className="flex items-center gap-2 mb-1.5 cursor-pointer"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      <svg 
                        className="w-3 h-3 transition-transform"
                        style={{ transform: showConditions ? 'rotate(90deg)' : 'rotate(0deg)' }}
                        viewBox="0 0 24 24" 
                        fill="none" 
                        stroke="currentColor" 
                        strokeWidth="2" 
                        strokeLinecap="round" 
                        strokeLinejoin="round"
                      >
                        <path d="M9 18l6-6-6-6"/>
                      </svg>
                      <span className="text-[11px]">Conditions</span>
                    </button>
                    {showConditions && (
                    <div className="space-y-2">
                      <div className="grid grid-cols-2 gap-2">
                        <div>
                          <label
                            className="block text-[10px] mb-1"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Min new messages
                          </label>
                          <input
                            type="number"
                            min="0"
                            value={autoUpdateMinNewMessages || ''}
                            onChange={(e) => setAutoUpdateMinNewMessages(e.target.value ? parseInt(e.target.value) : undefined)}
                            placeholder="Optional"
                            className="w-full text-xs px-2 py-1 rounded"
                            style={{
                              backgroundColor: 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                              outline: 'none',
                            }}
                          />
                        </div>
                        <div>
                          <label
                            className="block text-[10px] mb-1"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Min context change (0-1)
                          </label>
                          <input
                            type="number"
                            min="0"
                            max="1"
                            step="0.1"
                            value={autoUpdateMinContextChange || ''}
                            onChange={(e) => setAutoUpdateMinContextChange(e.target.value ? parseFloat(e.target.value) : undefined)}
                            placeholder="Optional"
                            className="w-full text-xs px-2 py-1 rounded"
                            style={{
                              backgroundColor: 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                              outline: 'none',
                            }}
                          />
                        </div>
                      </div>
                      <div>
                        <label
                          className="block text-[10px] mb-1"
                          style={{ color: 'var(--text-secondary-strong)' }}
                        >
                          Keywords (comma-separated)
                        </label>
                        <input
                          type="text"
                          value={autoUpdateKeywords}
                          onChange={(e) => setAutoUpdateKeywords(e.target.value)}
                          placeholder="Optional"
                          className="w-full text-xs px-2 py-1 rounded"
                          style={{
                            backgroundColor: 'var(--white-06)',
                            border: '1px solid var(--overlay-line-90)',
                            color: 'var(--text-heading)',
                            outline: 'none',
                          }}
                        />
                      </div>
                      <label className="flex items-center gap-1.5 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={autoUpdateSearchQueryChanged}
                          onChange={(e) => setAutoUpdateSearchQueryChanged(e.target.checked)}
                          className="w-3 h-3"
                          style={{
                            accentColor: 'var(--accent-primary)',
                          }}
                        />
                        <span
                          className="text-[10px]"
                          style={{ color: 'var(--text-secondary-strong)' }}
                        >
                          Trigger if search query changed
                        </span>
                      </label>
                    </div>
                    )}
                  </div>

                  <div>
                    <label className="flex items-center gap-2 cursor-pointer mb-2">
                      <input
                        type="checkbox"
                        checked={autoUpdateCheckQuality}
                        onChange={(e) => setAutoUpdateCheckQuality(e.target.checked)}
                        className="w-3 h-3"
                        style={{
                          accentColor: 'var(--accent-primary)',
                        }}
                      />
                      <span
                        className="text-[11px]"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Smart update (check quality)
                      </span>
                    </label>
                    {autoUpdateCheckQuality && (
                      <div className="ml-5 mt-2 space-y-2">
                        <div>
                          <label
                            className="block text-[10px] mb-1"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Min quality threshold (0-1)
                          </label>
                          <input
                            type="number"
                            min="0"
                            max="1"
                            step="0.1"
                            value={autoUpdateMinQualityThreshold}
                            onChange={(e) => setAutoUpdateMinQualityThreshold(parseFloat(e.target.value))}
                            className="w-full text-xs px-2 py-1 rounded"
                            style={{
                              backgroundColor: 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                              color: 'var(--text-heading)',
                              outline: 'none',
                            }}
                          />
                        </div>
                        <label className="flex items-center gap-1.5 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={autoUpdateNotifyOnUpdate}
                            onChange={(e) => setAutoUpdateNotifyOnUpdate(e.target.checked)}
                            className="w-3 h-3"
                            style={{
                              accentColor: 'var(--accent-primary)',
                            }}
                          />
                          <span
                            className="text-[10px]"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            Notify on update
                          </span>
                        </label>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* Generate button */}
            <div className="pt-2 flex justify-end">
              <button
                onClick={handleGeneratePreview}
                disabled={!artifactName.trim() || generating}
                className="text-xs px-3 py-1.5 rounded transition-colors"
                style={{
                  backgroundColor: artifactName.trim() && !generating
                    ? 'var(--white-10)'
                    : 'var(--white-06)',
                  color: artifactName.trim() && !generating
                    ? 'var(--text-heading)'
                    : 'var(--text-tertiary-weak)',
                  border: '1px solid var(--overlay-line-90)',
                  cursor: artifactName.trim() && !generating ? 'pointer' : 'not-allowed',
                }}
                onMouseEnter={(e) => {
                  if (artifactName.trim() && !generating) {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
                  }
                }}
                onMouseLeave={(e) => {
                  if (artifactName.trim() && !generating) {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                  }
                }}
              >
                {generating 
                  ? `Generating... (${generationProgress.current}/${generationProgress.total})`
                  : 'Next'}
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Preview view - show generated content
  if (wizardStep === 'preview' && selectedTemplate) {
    const sortedSections = [...(selectedTemplate.defaultStructure || [])].sort(
      (a, b) => a.order - b.order
    );

    return (
      <div className="flex flex-col h-full" style={{ minHeight: 0 }}>
        {/* Back button */}
        <div className="pb-2">
          <button
            onClick={handleBack}
            className="flex items-center gap-2 text-xs"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            <svg
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M15 19l-7-7 7-7"
              />
            </svg>
            Back
          </button>
        </div>

        <div className="flex-1 overflow-y-auto space-y-4 pt-2">
          {/* Content name */}
          <div>
            <h3
              className="text-sm font-medium mb-1"
              style={{ color: 'var(--text-heading)' }}
            >
              {artifactName}
            </h3>
            <p
              className="text-xs"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Preview of generated content
            </p>
          </div>

          {/* Sections preview */}
          <div className="space-y-4">
            {sortedSections.map((section) => {
              const generated = generatedSections[section.id];
              return (
                <div
                  key={section.id}
                  className="rounded-lg"
                  style={{
                    backgroundColor: 'var(--toolbar-bg)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                >
                  {/* Section header */}
                  <div
                    className="px-4 py-3 border-b"
                    style={{ borderColor: 'var(--overlay-line-90)' }}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex-1">
                        <h4
                          className="text-sm font-medium"
                          style={{ color: 'var(--text-heading)' }}
                        >
                          {section.title}
                          {section.required && (
                            <span
                              className="ml-2 text-xs"
                              style={{ color: 'var(--text-tertiary-weak)' }}
                            >
                              (required)
                            </span>
                          )}
                        </h4>
                        {section.description && (
                          <p
                            className="text-xs mt-1"
                            style={{ color: 'var(--text-secondary-strong)' }}
                          >
                            {section.description}
                          </p>
                        )}
                      </div>
                      <button
                        onClick={() => handleRefreshSection(section.id)}
                        disabled={refreshingSectionId === section.id}
                        className="flex-shrink-0 text-xs px-2 py-1 rounded transition-colors"
                        style={{
                          backgroundColor: refreshingSectionId === section.id
                            ? 'var(--white-06)'
                            : 'transparent',
                          color: refreshingSectionId === section.id
                            ? 'var(--text-tertiary-weak)'
                            : 'var(--text-secondary-strong)',
                          border: '1px solid var(--overlay-line-90)',
                          cursor: refreshingSectionId === section.id ? 'not-allowed' : 'pointer',
                        }}
                        onMouseEnter={(e) => {
                          if (refreshingSectionId !== section.id) {
                            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                          }
                        }}
                        onMouseLeave={(e) => {
                          if (refreshingSectionId !== section.id) {
                            (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                          }
                        }}
                        title="Refresh section"
                      >
                        <svg
                          className={`w-3.5 h-3.5 ${refreshingSectionId === section.id ? 'animate-spin' : ''}`}
                          fill="none"
                          stroke="currentColor"
                          viewBox="0 0 24 24"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth={2}
                            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
                          />
                        </svg>
                      </button>
                    </div>
                  </div>

                  {/* Section content */}
                  <div className="px-4 py-3">
                    {refreshingSectionId === section.id ? (
                      <div className="space-y-2">
                        <div className="animate-pulse">
                          <div className="h-4 bg-gray-200 rounded w-3/4 mb-2" style={{ backgroundColor: 'var(--white-10)' }}></div>
                          <div className="h-4 bg-gray-200 rounded w-full mb-2" style={{ backgroundColor: 'var(--white-10)' }}></div>
                          <div className="h-4 bg-gray-200 rounded w-5/6 mb-2" style={{ backgroundColor: 'var(--white-10)' }}></div>
                          <div className="h-4 bg-gray-200 rounded w-4/5 mb-2" style={{ backgroundColor: 'var(--white-10)' }}></div>
                          <div className="h-4 bg-gray-200 rounded w-3/4" style={{ backgroundColor: 'var(--white-10)' }}></div>
                        </div>
                      </div>
                    ) : generating && !generated ? (
                      <div
                        className="text-xs py-4 text-center"
                        style={{ color: 'var(--text-secondary-strong)' }}
                      >
                        Generating...
                      </div>
                    ) : generated?.error ? (
                      <div
                        className="text-xs py-2"
                        style={{ color: 'var(--accent-danger)' }}
                      >
                        Error: {generated.error}
                      </div>
                    ) : generated?.content ? (
                      <div className="markdown text-xs" style={{ color: 'var(--text-heading)' }}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>
                          {generated.content}
                        </ReactMarkdown>
                      </div>
                    ) : (
                      <div
                        className="text-xs py-2"
                        style={{ color: 'var(--text-tertiary-weak)' }}
                      >
                        No content generated
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          {/* Save button */}
          <div className="pt-4 flex justify-end gap-2">
            <button
              onClick={handleBack}
              className="text-xs px-3 py-1.5 rounded transition-colors"
              style={{
                backgroundColor: 'transparent',
                color: 'var(--text-secondary-strong)',
                border: '1px solid var(--overlay-line-90)',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }}
            >
              Back
            </button>
            <button
              onClick={handleSaveArtifact}
              disabled={creating}
              className="text-xs px-3 py-1.5 rounded transition-colors"
              style={{
                backgroundColor: !creating
                  ? 'var(--white-10)'
                  : 'var(--white-06)',
                color: !creating
                  ? 'var(--text-heading)'
                  : 'var(--text-tertiary-weak)',
                border: '1px solid var(--overlay-line-90)',
                cursor: !creating ? 'pointer' : 'not-allowed',
              }}
              onMouseEnter={(e) => {
                if (!creating) {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
                }
              }}
              onMouseLeave={(e) => {
                if (!creating) {
                  (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                }
              }}
            >
              {creating ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full" style={{ minHeight: 0 }}>
      <div className="flex-1 overflow-y-auto space-y-4 pt-2">
      {/* Dynamic Content (Artifacts) section */}
      {wizardStep === 'templates' && (
      <div>
        <div className="flex items-center justify-between mb-2">
          <h4
            className="text-[11px] uppercase tracking-wide"
            style={{ color: 'var(--text-tertiary-weak)' }}
          >
            Dynamic Content ({artifacts.length})
          </h4>
        </div>
        {artifacts.length === 0 ? (
          <div
            className="text-xs py-4 text-center"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            No dynamic content yet. Create one from a template below.
          </div>
        ) : (
          <div className="space-y-1">
            {artifacts.map((artifact) => (
              <div
                key={artifact.id}
                className="px-2 py-1.5 rounded text-xs flex items-center gap-2 group"
                style={{
                  backgroundColor: 'transparent',
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLDivElement).style.backgroundColor =
                    'var(--white-10)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLDivElement).style.backgroundColor =
                    'transparent';
                }}
              >
                <div
                  className="flex items-center gap-2 flex-1 min-w-0 cursor-pointer"
                  onClick={() => {
                    if (editingArtifactId !== artifact.id) {
                      onOpenDocument(artifact);
                    }
                  }}
                >
                  <div style={{ color: 'var(--text-secondary)' }}>
                    {getArtifactIcon()}
                  </div>
                  {editingArtifactId === artifact.id ? (
                    <input
                      ref={editingArtifactInputRef}
                      type="text"
                      value={editingArtifactName}
                      onChange={(e) => setEditingArtifactName(e.target.value)}
                      onKeyDown={async (e) => {
                        if (e.key === 'Enter') {
                          e.stopPropagation();
                          if (!projectId || projectId === 'local-unfold') return;
                          const newName = editingArtifactName.trim();
                          if (newName && newName !== artifact.filename) {
                            try {
                              await api.docsUpdate(projectId, artifact.id, { filename: newName });
                              const docs = await api.docsList(projectId);
                              const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
                              setArtifacts(artifactsList);
                              window.dispatchEvent(new CustomEvent('content.updated', { detail: { projectId } }));
                            } catch (err) {
                              console.error('[DynamicContentPanel] Failed to rename artifact', err);
                              alert('Failed to rename content');
                            }
                          }
                          setEditingArtifactId(null);
                          setEditingArtifactName('');
                        } else if (e.key === 'Escape') {
                          e.stopPropagation();
                          setEditingArtifactId(null);
                          setEditingArtifactName('');
                        }
                      }}
                      onBlur={async () => {
                        if (!projectId || projectId === 'local-unfold') return;
                        const newName = editingArtifactName.trim();
                        if (newName && newName !== artifact.filename) {
                          try {
                            await api.docsUpdate(projectId, artifact.id, { filename: newName });
                            const docs = await api.docsList(projectId);
                            const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
                            setArtifacts(artifactsList);
                            window.dispatchEvent(new CustomEvent('content.updated', { detail: { projectId } }));
                          } catch (err) {
                            console.error('[DynamicContentPanel] Failed to rename artifact', err);
                            alert('Failed to rename content');
                          }
                        }
                        setEditingArtifactId(null);
                        setEditingArtifactName('');
                      }}
                      className="flex-1 px-1 py-0.5 rounded text-xs"
                      style={{
                        backgroundColor: 'var(--bg-input, #1A1A1A)',
                        color: 'var(--text-heading)',
                        border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                      }}
                      onClick={(e) => e.stopPropagation()}
                    />
                  ) : (
                    <>
                      <span
                        className="flex-1 truncate"
                        style={{ color: 'var(--text-heading)' }}
                        title={artifact.filename}
                      >
                        {artifact.filename}
                      </span>
                      {artifact.updatedAt && (
                        <span
                          className="text-[10px]"
                          style={{ color: 'var(--text-tertiary-weak)' }}
                        >
                          {formatDate(artifact.updatedAt)}
                        </span>
                      )}
                    </>
                  )}
                </div>
                <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      setEditingArtifactId(artifact.id);
                      setEditingArtifactName(artifact.filename);
                      setTimeout(() => editingArtifactInputRef.current?.focus(), 0);
                    }}
                    className="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                    style={{ color: 'var(--text-secondary)' }}
                    aria-label="Rename content"
                    title="Rename content"
                  >
                    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    onClick={async (e) => {
                      e.stopPropagation();
                      if (!projectId || projectId === 'local-unfold') return;
                      if (!confirm(`Delete "${artifact.filename}"?`)) return;
                      try {
                        await api.docsDelete(projectId, artifact.id);
                        const docs = await api.docsList(projectId);
                        const artifactsList = docs.filter((doc: CoreDoc) => isArtifact(doc)) as Artifact[];
                        setArtifacts(artifactsList);
                        window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
                      } catch (err) {
                        console.error('[DynamicContentPanel] Failed to delete artifact', err);
                        alert('Failed to delete content');
                      }
                    }}
                    className="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                    style={{ color: 'var(--text-secondary)' }}
                    aria-label="Delete content"
                    title="Delete content"
                  >
                    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M6 18L18 6M6 6l12 12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      )}

      {/* Templates section */}
      {wizardStep === 'templates' && (
      <div className="mt-6 space-y-6">
        {/* System Templates */}
        <div>
        <div className="mb-2">
          <h4
            className="text-[11px] uppercase tracking-wide"
            style={{ color: 'var(--text-tertiary-weak)' }}
          >
              System Templates ({systemTemplates.length})
          </h4>
        </div>
        <button
          type="button"
          onClick={() => setShowTemplates(!showTemplates)}
          className="w-full flex items-center justify-between px-2 py-1.5 rounded text-xs"
          style={{
            backgroundColor: 'transparent',
            color: 'var(--text-heading)',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
        >
          <div className="flex items-center gap-2">
            <div style={{ color: 'var(--text-secondary)' }}>
              {getFolderIcon()}
            </div>
              <span>{showTemplates ? 'Hide system templates' : 'Show system templates'}</span>
          </div>
          <svg
            className="w-3 h-3 transition-transform"
            style={{
              transform: showTemplates ? 'rotate(180deg)' : 'rotate(0deg)',
              color: 'var(--text-secondary)',
            }}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              d="M6 9l6 6 6-6"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </button>

        {showTemplates && (
          <div className="mt-2">
              {systemTemplates.length === 0 ? (
                <div
                  className="text-xs py-2 text-center"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  No system templates available
                </div>
              ) : (
            <TemplatesList
              onTemplateSelect={handleTemplateSelect}
                  templates={systemTemplates}
                  groupedTemplates={groupedSystemTemplates}
            />
              )}
          </div>
        )}
      </div>

        {/* Custom Templates */}
        <div>
          <div className="mb-2 flex items-center justify-between">
            <h4
              className="text-[11px] uppercase tracking-wide"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              Custom Templates ({customTemplates.length})
            </h4>
          </div>

          {/* Custom Templates List */}
          {customTemplates.length === 0 ? (
            <div
              className="text-xs py-2 text-left"
              style={{ 
                color: 'var(--text-secondary-strong)',
              }}
            >
              No custom templates yet. Create your first one below.
            </div>
          ) : (
            <TemplatesList
              onTemplateSelect={handleTemplateSelect}
              templates={customTemplates}
              groupedTemplates={groupedCustomTemplates}
              widerColumns={true}
              isCustomTemplates={true}
              onEditTemplate={(template) => {
                setEditingTemplateId(template.id);
                setEditingTemplateName(template.name);
                setEditingTemplateDescription(template.description || '');
                setEditingTemplateSections([...(template.defaultStructure || [])].sort((a, b) => a.order - b.order));
              }}
              onDeleteTemplate={async (templateId) => {
                              const template = customTemplates.find(t => t.id === templateId);
                              if (!template) return;
                              
                              if (!confirm(t('artifacts.confirmDeleteTemplate', { name: template.name, defaultValue: `Are you sure you want to delete "${template.name}"?` }))) {
                                return;
                              }
                
                setDeletingTemplateId(templateId);
                try {
                  await api.deleteArtifactTemplate(templateId);
                  
                  // Reload templates
                  const templatesList = await api.listArtifactTemplates();
                  setTemplates(templatesList);
                  
                  alert('Template deleted successfully!');
                } catch (error: any) {
                  console.error('Error deleting template:', error);
                  alert(`Failed to delete template: ${error.message || 'Unknown error'}`);
                } finally {
                  setDeletingTemplateId(null);
                }
              }}
              editingTemplateId={editingTemplateId}
              deletingTemplateId={deletingTemplateId}
            />
          )}

          {/* Create Custom Template Button */}
          <div className="mt-3">
            <button
              type="button"
              onClick={() => {
                setShowCreateTemplateWizard(true);
                setTemplateUserRequest('');
              }}
              className="inline-flex items-center gap-2 px-2 py-1.5 rounded text-xs"
              style={{
                backgroundColor: 'var(--white-10)',
                color: 'var(--text-heading)',
                border: '1px dashed var(--overlay-line-90)',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  'var(--white-15)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor =
                  'var(--white-10)';
              }}
            >
              <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M12 5v14M5 12h14" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span>Create custom template</span>
            </button>
          </div>
        </div>
      </div>
      )}

      {/* Create Template Wizard */}
      {wizardStep === 'templates' && showCreateTemplateWizard && (
        <div className="mt-6">
          <div className="mb-4">
            <div className="flex items-center justify-between mb-2">
              <h4
                className="text-[11px] uppercase tracking-wide"
                style={{ color: 'var(--text-tertiary-weak)' }}
              >
                Create Custom Template
              </h4>
              <button
                type="button"
                onClick={() => {
                  setShowCreateTemplateWizard(false);
                  setTemplateUserRequest('');
                }}
                className="text-xs"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                Cancel
              </button>
            </div>
            
            {/* Explanation */}
            <div
              className="text-xs mb-3"
              style={{
                color: 'var(--text-secondary-strong)',
              }}
            >
              {t('artifacts.templateCreationHint', { defaultValue: 'The system can automatically create a template based on your request.' })}
            </div>

            {/* User request field */}
            <div>
              <label
                className="block text-13 mb-2"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                Template request
              </label>
              <textarea
                value={templateUserRequest}
                onChange={(e) => setTemplateUserRequest(e.target.value)}
                placeholder="Describe what kind of template you need. For example: 'Meeting summary template with action items and decisions'..."
                rows={4}
                className="w-full text-xs resize-none"
                style={{
                  backgroundColor: 'var(--bg-input, transparent)',
                  border: '1px solid var(--border-input, var(--overlay-line-90))',
                  borderRadius: '8px',
                  padding: '10px 12px',
                  outline: 'none',
                  color: 'var(--text-heading)',
                  transition: 'all 0.2s ease',
                }}
                onFocus={(e) => {
                  e.currentTarget.style.borderColor = 'var(--text-secondary)';
                  e.currentTarget.style.backgroundColor = 'var(--white-06)';
                }}
                onBlur={(e) => {
                  e.currentTarget.style.borderColor = 'var(--overlay-line-90)';
                  e.currentTarget.style.backgroundColor = 'var(--bg-input, transparent)';
                }}
              />
              <div className="mt-3 flex flex-col items-start gap-2">
                <button
                  type="button"
                  disabled={isGeneratingTemplate}
                  onClick={async () => {
                    if (!templateUserRequest.trim()) {
                      alert('Please enter a template request');
                      return;
                    }
                    setIsGeneratingTemplate(true);
                    try {
                      // Generate template structure based on user request
                      const prompt = `You are an expert at creating document templates for a dynamic document generation system.

Context:
This system creates templates for dynamic documents. The system allows transforming traditional discussions into various artifacts, including documents. The system will automatically provide all necessary informational context for document generation. Your task is to create a document template structure based on the user's request.

User Request: ${templateUserRequest}

Generate a JSON object with the following structure:
{
  "name": "Template name (concise, descriptive, 3-8 words that capture the essence of the template)",
  "sections": [
    {
      "id": "unique identifier (lowercase, underscores, e.g., \"executive_summary\")",
      "title": "section title (clear and descriptive)",
      "order": numeric order (starting from 1),
      "description": "what this section should contain (1-2 sentences)",
      "promptTemplate": "detailed prompt for generating this section's content (should be comprehensive and specific, 2-4 sentences)",
      "required": boolean indicating if section is required (true for essential sections, false for optional),
      "editable": boolean indicating if user can edit/remove (usually true)
    }
  ]
}

Requirements:
- The template name should be concise, professional, and clearly describe what kind of document this template creates
- The structure should be logical, comprehensive, and appropriate for the user's request
- Include 4-8 sections depending on the complexity of the request
- Each section should have a clear purpose and contribute to the overall document
- Prompts should be detailed enough to guide content generation
- Section order should follow logical flow (introduction → main content → conclusion)

Return ONLY a valid JSON object. Do not include markdown code fences or any additional text.

Example format:
{
  "name": "Meeting Summary Template",
  "sections": [
    {
      "id": "executive_summary",
      "title": "Executive Summary",
      "order": 1,
      "description": "High-level overview for decision makers",
      "promptTemplate": "Provide a concise executive summary that highlights the key points, main conclusions, and recommendations. Focus on the most important information that decision makers need to know.",
      "required": true,
      "editable": true
    }
  ]
}`;

                      const response = await api.aiStructured({
                        prompt,
                        minCapabilityScore: 7,
                        maxOutputSize: 8192,
                      });

                      if (!response.ok || !response.result) {
                        throw new Error('Failed to generate template structure');
                      }

                      // Parse and validate response
                      const result = response.result;
                      if (!result || typeof result !== 'object') {
                        throw new Error('Invalid response format: expected object');
                      }

                      // Extract name and sections
                      const templateName = result.name || templateUserRequest.trim().slice(0, 50) || 'Generated Template';
                      let sections = result.sections;
                      
                      if (!Array.isArray(sections)) {
                        throw new Error('Invalid response format: expected sections array');
                      }

                      // Validate each section has required fields
                      sections = sections.map((section: any, index: number) => ({
                        id: section.id || `section_${index + 1}`,
                        title: section.title || `Section ${index + 1}`,
                        order: typeof section.order === 'number' ? section.order : index + 1,
                        description: section.description || '',
                        promptTemplate: section.promptTemplate || section.prompt || '',
                        required: section.required !== undefined ? section.required : true,
                        editable: section.editable !== undefined ? section.editable : true,
                      }));

                      // Sort by order
                      sections.sort((a: any, b: any) => a.order - b.order);

                      // Store generated structure and name for display in template creation wizard
                      setGeneratedTemplateStructure(sections);
                      setGeneratedTemplateName(templateName);
                      
                      console.log('Generated template structure:', { name: templateName, sections });
                    } catch (error: any) {
                      console.error('Error generating template structure:', error);
                      alert(`Failed to generate template structure: ${error.message || 'Unknown error'}`);
                    } finally {
                      setIsGeneratingTemplate(false);
                    }
                  }}
                  className="inline-flex items-center justify-center gap-2 px-3 py-1.5 rounded text-xs"
                  style={{
                    backgroundColor: isGeneratingTemplate ? 'var(--white-05)' : 'var(--white-10)',
                    color: isGeneratingTemplate ? 'var(--text-tertiary-weak)' : 'var(--text-heading)',
                    border: '1px solid var(--overlay-line-90)',
                    cursor: isGeneratingTemplate ? 'not-allowed' : 'pointer',
                    opacity: isGeneratingTemplate ? 0.6 : 1,
                  }}
                  onMouseEnter={(e) => {
                    if (!isGeneratingTemplate) {
                      e.currentTarget.style.backgroundColor = 'var(--white-15)';
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (!isGeneratingTemplate) {
                      e.currentTarget.style.backgroundColor = 'var(--white-10)';
                    }
                  }}
                >
                  {isGeneratingTemplate ? 'Generating...' : t('artifacts.createTemplateBasedOnRequest', { defaultValue: 'Create template based on request' })}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    // TODO: Switch to manual template creation mode
                    console.log('Manual template creation clicked');
                  }}
                  className="text-xs text-left"
                  style={{
                    color: 'var(--text-secondary-strong)',
                    backgroundColor: 'transparent',
                    border: 'none',
                    padding: 0,
                    cursor: 'pointer',
                    textDecoration: 'underline',
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.color = 'var(--text-heading)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.color = 'var(--text-secondary-strong)';
                  }}
                >
                  {t('artifacts.createTemplateManually', { defaultValue: 'I prefer to create the template manually' })}
                </button>
              </div>

              {/* Generated Template Structure Display */}
              {generatedTemplateStructure && generatedTemplateStructure.length > 0 && (
                <div className="mt-4 space-y-4">
                  {/* Template Name */}
                  {generatedTemplateName && (
                    <div>
                      <div
                        className="text-xs font-medium mb-1"
                        style={{ color: 'var(--text-tertiary-weak)' }}
                      >
                        Template Name
                      </div>
                      <div
                        className="text-sm font-semibold"
                        style={{ color: 'var(--text-heading)' }}
                      >
                        {generatedTemplateName}
                      </div>
                    </div>
                  )}

                  {/* Sections List */}
                  <div>
                    <label
                      className="block text-13 mb-2"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Sections ({generatedTemplateStructure.length})
                    </label>
                    <div className="space-y-2">
                      {generatedTemplateStructure.map((section: any, index: number) => {
                        const isExpanded = expandedSections[`gen-${section.id}`] || false;
                        return (
                          <div
                            key={section.id}
                            className="px-3 py-2 rounded group"
                            style={{
                              backgroundColor: 'var(--white-06)',
                              border: '1px solid var(--overlay-line-90)',
                            }}
                          >
                            <div className="flex items-start justify-between gap-2">
                              <div className="flex-1 min-w-0">
                                <div
                                  className="text-xs font-medium mb-1"
                                  style={{ color: 'var(--text-heading)' }}
                                >
                                  {section.title}
                                  {section.required && (
                                    <span
                                      className="ml-1 text-[10px]"
                                      style={{ color: 'var(--text-tertiary-weak)' }}
                                    >
                                      (required)
                                    </span>
                                  )}
                                </div>
                                {section.description && (
                                  <div
                                    className="text-[10px] mb-1"
                                    style={{ color: 'var(--text-secondary-strong)' }}
                                  >
                                    {section.description}
                                  </div>
                                )}
                                {section.promptTemplate && !isExpanded && (
                                  <div
                                    className="text-[10px] mt-1 pt-1 line-clamp-2"
                                    style={{
                                      color: 'var(--text-tertiary-weak)',
                                      borderTop: '1px solid var(--overlay-line-90)',
                                      fontStyle: 'italic',
                                    }}
                                  >
                                    {section.promptTemplate}
                                  </div>
                                )}
                                {isExpanded && (
                                  <div className="mt-2 pt-2 border-t" style={{ borderColor: 'var(--overlay-line-90)' }}>
                                    <label className="block text-[10px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                      Prompt Template
                                    </label>
                                    <textarea
                                      value={section.promptTemplate || ''}
                                      onChange={(e) => {
                                        const updated = [...generatedTemplateStructure];
                                        updated[index].promptTemplate = e.target.value;
                                        setGeneratedTemplateStructure(updated);
                                      }}
                                      rows={4}
                                      className="w-full px-2 py-1 rounded text-xs"
                                      style={{
                                        backgroundColor: 'var(--bg-input, #1A1A1A)',
                                        color: 'var(--text-heading)',
                                        border: '1px solid var(--border-input, rgba(255, 255, 255, 0.1))',
                                        resize: 'vertical',
                                      }}
                                    />
                                  </div>
                                )}
                              </div>
                              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                                <button
                                  type="button"
                                  onClick={() => {
                                    setExpandedSections((prev) => ({
                                      ...prev,
                                      [`gen-${section.id}`]: !prev[`gen-${section.id}`],
                                    }));
                                  }}
                                  className="p-1 rounded"
                                  title={isExpanded ? 'Collapse' : 'Edit prompt'}
                                  style={{ backgroundColor: 'transparent' }}
                                  onMouseEnter={(e) => {
                                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                                  }}
                                  onMouseLeave={(e) => {
                                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                                  }}
                                >
                                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                    <path
                                      strokeLinecap="round"
                                      strokeLinejoin="round"
                                      strokeWidth={2}
                                      d={isExpanded 
                                        ? "M6 9l6 6 6-6" 
                                        : "M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                                      }
                                    />
                                  </svg>
                                </button>
                                <button
                                  type="button"
                                  onClick={() => {
                                    if (confirm(t('artifacts.confirmDeleteSection', { name: section.title, defaultValue: `Are you sure you want to delete section "${section.title}"?` }))) {
                                      const updated = generatedTemplateStructure.filter((s: any) => s.id !== section.id);
                                      setGeneratedTemplateStructure(updated);
                                    }
                                  }}
                                  className="p-1 rounded"
                                  title={t('artifacts.deleteSection', { defaultValue: 'Delete section' })}
                                  style={{ backgroundColor: 'transparent' }}
                                  onMouseEnter={(e) => {
                                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                                  }}
                                  onMouseLeave={(e) => {
                                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                                  }}
                                >
                                  <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                    <path
                                      strokeLinecap="round"
                                      strokeLinejoin="round"
                                      strokeWidth={2}
                                      d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12"
                                    />
                                  </svg>
                                </button>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  {/* Save Button */}
                  <div className="pt-2">
                    <button
                      type="button"
                      disabled={isSavingTemplate}
                      onClick={async () => {
                        if (!generatedTemplateName.trim() || !generatedTemplateStructure || generatedTemplateStructure.length === 0) {
                          alert('Template structure is incomplete');
                          return;
                        }
                        setIsSavingTemplate(true);
                        try {
                          const templateToSave = {
                            name: generatedTemplateName,
                            description: `Template generated based on: ${templateUserRequest.trim().slice(0, 100)}`,
                            category: 'Custom',
                            type: 'custom',
                            defaultStructure: generatedTemplateStructure.map((section: any) => ({
                              id: section.id,
                              title: section.title,
                              order: section.order,
                              description: section.description || '',
                              promptTemplate: section.promptTemplate || section.prompt || '',
                              required: section.required !== undefined ? section.required : true,
                              editable: section.editable !== undefined ? section.editable : true,
                            })),
                            recommendedFormat: 'text',
                            recommendedFocus: 'analysis',
                          };

                          await api.createArtifactTemplate(templateToSave);
                          
                          // Reload templates list
                          const templatesList = await api.listArtifactTemplates();
                          setTemplates(templatesList);
                          
                          // Reset wizard state
                          setGeneratedTemplateStructure(null);
                          setGeneratedTemplateName('');
                          setTemplateUserRequest('');
                          setShowCreateTemplateWizard(false);
                          
                          alert('Template saved successfully!');
                        } catch (error: any) {
                          console.error('Error saving template:', error);
                          alert(`Failed to save template: ${error.message || 'Unknown error'}`);
                        } finally {
                          setIsSavingTemplate(false);
                        }
                      }}
                      className="inline-flex items-center justify-center gap-2 px-4 py-2 rounded text-xs font-medium"
                      style={{
                        backgroundColor: 'var(--accent-primary)',
                        color: 'white',
                        border: 'none',
                        cursor: isSavingTemplate ? 'not-allowed' : 'pointer',
                        opacity: isSavingTemplate ? 0.6 : 1,
                      }}
                      onMouseEnter={(e) => {
                        if (!isSavingTemplate) {
                          e.currentTarget.style.opacity = '0.9';
                        }
                      }}
                      onMouseLeave={(e) => {
                        if (!isSavingTemplate) {
                          e.currentTarget.style.opacity = '1';
                        }
                      }}
                    >
                      {isSavingTemplate ? 'Saving...' : 'Save to templates'}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
      </div>
    </div>
  );
};
