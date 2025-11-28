import React from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { isArtifact, type Artifact } from '../types/artifact';
import type { CoreDoc } from '../types/generated';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getPreferredLanguage, getLanguageName } from '../lib/uiUtils';
import { useGenaiChatStore } from '../stores/genaiChatStore';

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

interface PagesPanelProps {
  projectId: string | null;
  onOpenDocument: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
  onCreateFromTemplate?: (templateId: string) => void;
}

export const PagesPanel: React.FC<PagesPanelProps> = ({
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
  const [autoUpdateTrigger, setAutoUpdateTrigger] = React.useState<'on_new_message' | 'daily' | 'weekly' | 'on_new_document'>('on_new_message');
  const [expandedSections, setExpandedSections] = React.useState<Record<string, boolean>>({});
  const [autoUpdateMinNewMessages, setAutoUpdateMinNewMessages] = React.useState<number | undefined>(undefined);
  const [autoUpdateMinContextChange, setAutoUpdateMinContextChange] = React.useState<number | undefined>(undefined);
  const [autoUpdateKeywords, setAutoUpdateKeywords] = React.useState<string>('');
  const [autoUpdateSearchQueryChanged, setAutoUpdateSearchQueryChanged] = React.useState(false);
  const [autoUpdateCheckQuality, setAutoUpdateCheckQuality] = React.useState(false);
  const [autoUpdateMinQualityThreshold, setAutoUpdateMinQualityThreshold] = React.useState<number>(0.7);
  const [autoUpdateNotifyOnUpdate, setAutoUpdateNotifyOnUpdate] = React.useState(false);

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
        console.error('[PagesPanel] Failed to load artifacts', err);
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
        setTemplates(templatesList || []);
      } catch (err) {
        console.error('[PagesPanel] Failed to load templates', err);
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

  // Group templates by category and sort alphabetically within each category
  const groupedTemplates = React.useMemo(() => {
    const grouped = templates.reduce((acc, template) => {
      const category = template.category || 'Other';
      if (!acc[category]) {
        acc[category] = [];
      }
      acc[category].push(template);
      return acc;
    }, {} as Record<string, ArtifactTemplate[]>);

    // Sort categories alphabetically and templates within each category
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
  }, [templates]);

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
      setAutoUpdateTrigger('on_new_message');
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
      console.error('[PagesPanel] Failed to load template', err);
    }
  };

  const handleBack = () => {
    if (wizardStep === 'preview') {
      setWizardStep('configure');
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
    } else if (wizardStep === 'configure') {
      setWizardStep('templates');
      setSelectedTemplate(null);
      setArtifactName('');
      setUserRequest('');
      setSectionAutoUpdate({});
      setOrchestratorModel('');
      setDefaultModel('');
      setSectionModels({});
      setDocumentAutoUpdateEnabled(false);
      setAutoUpdateTrigger('on_new_message');
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
          console.error(`[PagesPanel] Failed to generate section ${section.id}`, err);
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
      console.error('[PagesPanel] Failed to generate preview', err);
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
      console.error(`[PagesPanel] Failed to refresh section ${sectionId}`, err);
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

      const autoUpdateConfig = documentAutoUpdateEnabled ? {
        trigger: autoUpdateTrigger,
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
      window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));

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
      setAutoUpdateTrigger('on_new_message');
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
      onOpenDocument(createdDoc);
    } catch (err) {
      console.error('[PagesPanel] Failed to create artifact', err);
    } finally {
      setCreating(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="animate-pulse text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
          Loading pages...
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

  // Templates list component (reusable)
  const TemplatesList = ({ 
    compact = false, 
    selectedTemplateId,
    onTemplateSelect 
  }: { 
    compact?: boolean;
    selectedTemplateId?: string | null;
    onTemplateSelect: (templateId: string) => void;
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
    if (templates.length === 0) {
      return (
        <div
          className="text-xs py-2 text-center"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          No templates available
        </div>
      );
    }

    if (compact) {
      // Compact vertical list for desktop sidebar
      return (
        <div className="space-y-1">
          {groupedTemplates.map(({ category, templates: categoryTemplates }) => (
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
        {groupedTemplates.map(({ category, templates: categoryTemplates }) => (
          <div key={category}>
            <div
              className="text-[10px] uppercase tracking-wide mb-1.5"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              {category}
            </div>
            <div 
              className="grid gap-1"
              style={{
                gridTemplateColumns: 'repeat(auto-fill, minmax(200px, max-content))',
              }}
            >
              {categoryTemplates.map((template) => (
                <div
                  key={template.id}
                  className="px-2 py-1.5 rounded text-xs flex items-start gap-2 group cursor-pointer"
                  style={{
                    backgroundColor: 'transparent',
                    maxWidth: 'fit-content',
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLDivElement).style.backgroundColor =
                      'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLDivElement).style.backgroundColor =
                      'transparent';
                  }}
                  onClick={() => onTemplateSelect(template.id)}
                >
                  <div className="flex-shrink-0 mt-0.5" style={{ color: 'var(--text-secondary)' }}>
                    {getTemplateIcon()}
                  </div>
                  <div className="min-w-0" style={{ maxWidth: '300px' }}>
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
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    );
  };

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
            Page name
          </label>
          <input
            type="text"
            value={artifactName}
            onChange={(e) => setArtifactName(e.target.value)}
            placeholder="Enter page name"
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
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                  <option value="on_new_document">On new document</option>
                </select>
              </div>

              <div>
                <label
                  className="block text-[11px] mb-1.5"
                  style={{ color: 'var(--text-secondary-strong)' }}
                >
                  Conditions
                </label>
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
                Page name
              </label>
              <input
                type="text"
                value={artifactName}
                onChange={(e) => setArtifactName(e.target.value)}
                placeholder="Enter page name"
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
                      <option value="daily">Daily</option>
                      <option value="weekly">Weekly</option>
                      <option value="on_new_document">On new document</option>
                    </select>
                  </div>

                  <div>
                    <label
                      className="block text-[11px] mb-1.5"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      Conditions
                    </label>
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
          {/* Page name */}
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
      {/* Pages (Artifacts) section */}
      {wizardStep === 'templates' && (
      <div>
        <div className="flex items-center justify-between mb-2">
          <h4
            className="text-[11px] uppercase tracking-wide"
            style={{ color: 'var(--text-tertiary-weak)' }}
          >
            Pages ({artifacts.length})
          </h4>
        </div>
        {artifacts.length === 0 ? (
          <div
            className="text-xs py-4 text-center"
            style={{ color: 'var(--text-secondary-strong)' }}
          >
            No pages yet. Create one from a template below.
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
                              window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
                            } catch (err) {
                              console.error('[PagesPanel] Failed to rename artifact', err);
                              alert('Failed to rename page');
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
                            window.dispatchEvent(new CustomEvent('pages.updated', { detail: { projectId } }));
                          } catch (err) {
                            console.error('[PagesPanel] Failed to rename artifact', err);
                            alert('Failed to rename page');
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
                    aria-label="Rename page"
                    title="Rename page"
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
                        console.error('[PagesPanel] Failed to delete artifact', err);
                        alert('Failed to delete page');
                      }
                    }}
                    className="flex-shrink-0 w-4 h-4 flex items-center justify-center rounded-full hover:bg-white/10 transition-all"
                    style={{ color: 'var(--text-secondary)' }}
                    aria-label="Delete page"
                    title="Delete page"
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
      <div className="mt-6">
        <div className="mb-2">
          <h4
            className="text-[11px] uppercase tracking-wide"
            style={{ color: 'var(--text-tertiary-weak)' }}
          >
            Templates ({templates.length})
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
            <span>{showTemplates ? 'Hide templates' : 'Show templates'}</span>
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
            <TemplatesList
              onTemplateSelect={handleTemplateSelect}
            />
          </div>
        )}
      </div>
      )}
      </div>
    </div>
  );
};
