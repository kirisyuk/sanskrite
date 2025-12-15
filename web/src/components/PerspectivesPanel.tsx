import React from 'react';
import { api } from '../lib/api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from './ThemeProvider';
import { isArtifact, type Artifact } from '../types/artifact';
import type { CoreDoc } from '../types/generated';
import { getPreferredLanguage, getLanguageName } from '../lib/uiUtils';
import { mapAliasToModel } from '../lib/modelMapping';

interface ArtifactTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  type: 'system' | 'community' | 'custom';
  templateType?: string; // 'perspective' | 'document'
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
  recommendedOrchestratorModel?: string;
  recommendedSectionModel?: string;
}

interface PerspectivesPanelProps {
  onPresetChange?: (preset: string) => void;
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
}

const PerspectivesPanel: React.FC<PerspectivesPanelProps> = ({ onPresetChange, onOpenDocument }) => {
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const projectId = useGenaiChatStore((s) => s.projectId);
  const genAISettings = useGenAISettings();

  // Templates state
  const [templates, setTemplates] = React.useState<ArtifactTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = React.useState(false);
  const [selectedTemplate, setSelectedTemplate] = React.useState<ArtifactTemplate | null>(null);
  const [wizardStep, setWizardStep] = React.useState<'templates' | 'configure' | 'preview'>('templates');

  // Artifact creation state
  const [artifacts, setArtifacts] = React.useState<Artifact[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [artifactName, setArtifactName] = React.useState('');
  const [userRequest, setUserRequest] = React.useState('');
  const [creating, setCreating] = React.useState(false);
  const [orchestratorModel, setOrchestratorModel] = React.useState<string>('');
  const [defaultModel, setDefaultModel] = React.useState<string>('');
  const [sectionModels, setSectionModels] = React.useState<Record<string, string>>({});
  const [expandedSections, setExpandedSections] = React.useState<Record<string, boolean>>({});

  // Generation state
  const [generating, setGenerating] = React.useState(false);
  const [generatedSections, setGeneratedSections] = React.useState<Record<string, { content: string; error?: string }>>({});
  const [generationProgress, setGenerationProgress] = React.useState<{ current: number; total: number }>({ current: 0, total: 0 });
  const [refreshingSectionId, setRefreshingSectionId] = React.useState<string | null>(null);

  // Load artifacts (perspectives)
  React.useEffect(() => {
    if (!projectId || projectId === 'local-unfold') {
      setArtifacts([]);
      return;
    }

    const loadArtifacts = async () => {
      setLoading(true);
      try {
        const docs = await api.docsList(projectId);
        // Filter only perspective artifacts (created from perspective templates)
        const perspectivesList = docs.filter((doc: CoreDoc) => {
          if (!isArtifact(doc)) return false;
          // Check if artifact was created from a perspective template
          // We can identify this by checking if the artifact config has specific markers
          // For now, we'll filter by checking if the artifact name or structure suggests it's a perspective
          return true; // Show all artifacts for now, can be refined later
        }) as Artifact[];
        setArtifacts(perspectivesList);
      } catch (err) {
        console.error('[PerspectivesPanel] Failed to load perspectives', err);
        setArtifacts([]);
      } finally {
        setLoading(false);
      }
    };

    loadArtifacts();
  }, [projectId]);

  // Load templates (filter only perspective templates)
  React.useEffect(() => {
    const loadTemplates = async () => {
      setTemplatesLoading(true);
      try {
        const templatesList = await api.listArtifactTemplates();
        // Filter only templates with templateType === 'perspective'
        const perspectiveTemplates = (templatesList || []).filter((t: ArtifactTemplate) => 
          t.templateType === 'perspective' || (!t.templateType && t.category === 'perspective')
        );
        setTemplates(perspectiveTemplates);
      } catch (err) {
        console.error('[PerspectivesPanel] Failed to load templates', err);
        setTemplates([]);
      } finally {
        setTemplatesLoading(false);
      }
    };

    loadTemplates();
  }, []);

  // Separate templates into system and custom
  const { systemTemplates, customTemplates } = React.useMemo(() => {
    const system: ArtifactTemplate[] = [];
    const custom: ArtifactTemplate[] = [];
    
    templates.forEach(template => {
      if (template.type === 'system') {
        system.push(template);
      } else if (template.type === 'custom') {
        custom.push(template);
      }
    });
    
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
      
      // Initialize section models
      const initialSectionModels: Record<string, string> = {};
      (fullTemplate.defaultStructure || []).forEach((section: { id: string }) => {
        initialSectionModels[section.id] = recommendedSection.replace(/-/g, '_');
      });
      setSectionModels(initialSectionModels);
      
      // Initialize expanded sections (default: all collapsed)
      setExpandedSections({});
      
      // Notify parent about preset change
      onPresetChange?.(templateId);
    } catch (err) {
      console.error('[PerspectivesPanel] Failed to load template', err);
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
      setSectionModels({});
      setOrchestratorModel('');
      setDefaultModel('');
      setExpandedSections({});
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
      onPresetChange?.('');
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
    const chatContext: Array<{ role: string; content: string }> = [];
    const interactions = chatStore.interactions.filter((it: any) => {
      if (!it) return false;
      if (it.projectId !== projectId) return false;
      // Exclude perspective interactions
      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
        return false;
      }
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

    try {
      // Generate content for each section sequentially
      for (let i = 0; i < sortedSections.length; i++) {
        const section = sortedSections[i];
        const sectionModel = sectionModels[section.id] || defaultModel;
        
        setGenerationProgress({ current: i + 1, total: sortedSections.length });
        
        try {
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
          console.error(`[PerspectivesPanel] Failed to generate section ${section.id}`, err);
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
      console.error('[PerspectivesPanel] Failed to generate preview', err);
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
      const chatStore = useGenaiChatStore.getState();
      const preferredLang = getPreferredLanguage();
      const langName = getLanguageName(preferredLang);
      
      const chatContext: Array<{ role: string; content: string }> = [];
      const interactions = chatStore.interactions.filter((it: any) => {
        if (!it) return false;
        if (it.projectId !== projectId) return false;
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
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
      
      const recentContext = chatContext.slice(-10);
      const contextBlock = recentContext.length > 0
        ? `\n\nCHAT CONTEXT (recent conversation):\n${recentContext.map((m) => `${m.role}: ${m.content}`).join('\n')}`
        : '';

      const promptWithContext = section.promptTemplate + contextBlock;
      
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
      console.error(`[PerspectivesPanel] Failed to refresh section ${sectionId}`, err);
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
          autoUpdateEnabled: false, // Perspectives don't use auto-update by default
          content: generated?.content || '',
          generatedAt: generated?.content ? new Date().toISOString() : undefined,
        };
      });

      // Generate version ID
      const versionId = `v_${Date.now()}`;

      // Assemble full document from sections
      const sortedSections = [...artifactSections].sort((a, b) => a.order - b.order);
      const fullDocument = sortedSections
        .map(section => {
          if (!section.content) return '';
          return `## ${section.title}\n\n${section.content}`;
        })
        .filter(Boolean)
        .join('\n\n');

      const artifactDoc: Partial<CoreDoc> = {
        filename: artifactName.trim(),
        contentType: 'text/markdown',
        docType: 'dynamic_artifact',
        extractedText: fullDocument,
        artifactConfig: {
          format: (selectedTemplate.recommendedFormat || 'report') as 'text' | 'table' | 'report' | 'presentation',
          focus: (selectedTemplate.recommendedFocus || 'analysis') as 'brief' | 'analysis' | 'narrative' | 'proposal' | 'technical' | 'executive',
          orchestratorModel: orchestratorModel.replace(/_/g, '-'),
          defaultModel: defaultModel.replace(/_/g, '-'),
          reviewPrompt: 'Review and refine for clarity, consistency, and completeness.',
          structure: artifactSections,
          contextSources: {},
          autoUpdateEnabled: false,
          autoUpdateConfig: undefined,
          currentVersionID: versionId,
          versionsRetained: 10,
          versions: [],
          generationSettings: {
            preferredLanguage: getPreferredLanguage().toLowerCase(),
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
      setGeneratedSections({});
      setGenerationProgress({ current: 0, total: 0 });
      
      // Open the created document
      if (onOpenDocument && createdDoc) {
        onOpenDocument({
          id: createdDoc.id,
          filename: createdDoc.filename || artifactName.trim(),
          contentType: createdDoc.contentType,
          sizeBytes: createdDoc.sizeBytes,
          parseStatus: createdDoc.parseStatus,
          docType: createdDoc.docType,
          artifactConfig: createdDoc.artifactConfig,
        });
      }
      onPresetChange?.('');
    } catch (err) {
      console.error('[PerspectivesPanel] Failed to save perspective', err);
      alert('Failed to save perspective');
    } finally {
      setCreating(false);
    }
  };

  const getTemplateIcon = () => (
    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 2v6h6M12 18v-6M9 15h6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );

  const getArtifactIcon = () => (
    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
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

  // Templates list component (reusable, simplified from DynamicContentPanel)
  const TemplatesList = ({ 
    selectedTemplateId,
    onTemplateSelect,
    templates: templatesToShow,
    groupedTemplates: groupedTemplatesToShow,
  }: { 
    selectedTemplateId?: string | null;
    onTemplateSelect: (templateId: string) => void;
    templates: ArtifactTemplate[];
    groupedTemplates: Array<{ category: string; templates: ArtifactTemplate[] }>;
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
      return null;
    }

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
              {categoryTemplates.map((template) => (
                <div
                  key={template.id}
                  className="px-2 py-1.5 rounded text-xs flex items-start gap-2 group cursor-pointer"
                  style={{
                    backgroundColor: selectedTemplateId === template.id ? 'var(--white-10)' : 'transparent',
                  }}
                  onMouseEnter={(e) => {
                    if (selectedTemplateId !== template.id) {
                      (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)';
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (selectedTemplateId !== template.id) {
                      (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent';
                    }
                  }}
                  onClick={() => onTemplateSelect(template.id)}
                >
                  <div className="flex-shrink-0 mt-0.5" style={{ color: 'var(--text-secondary)' }}>
                    {getTemplateIcon()}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div
                      className="font-medium"
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
                  </div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    );
  };

  // Wizard view - configure perspective from template
  if (wizardStep === 'configure' && selectedTemplate) {
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
          {/* Template info */}
          <div>
            <h3
              className="text-sm font-medium mb-1"
              style={{ color: 'var(--text-heading)' }}
            >
              {selectedTemplate.name}
            </h3>
            {selectedTemplate.description && (
              <p
                className="text-xs"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                {selectedTemplate.description}
              </p>
            )}
          </div>

          {/* Perspective name input */}
          <div>
            <label
              className="block text-13 mb-2"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              Perspective name
            </label>
            <input
              type="text"
              value={artifactName}
              onChange={(e) => setArtifactName(e.target.value)}
              placeholder="Enter perspective name"
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
                  setSectionModels((prev) => {
                    const updated = { ...prev };
                    if (selectedTemplate?.defaultStructure) {
                      selectedTemplate.defaultStructure.forEach((section: { id: string }) => {
                        const currentModel = prev[section.id];
                        if (!currentModel || currentModel === oldDefault) {
                          updated[section.id] = newDefault;
                        }
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
                          <path d="M6 9l6 6 6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      </button>
                      {isExpanded && (
                        <div className="mt-2 pt-2 border-t" style={{ borderColor: 'var(--overlay-line-90)' }}>
                          {section.description && (
                            <p
                              className="text-[10px] mb-2"
                              style={{ color: 'var(--text-secondary-strong)' }}
                            >
                              {section.description}
                            </p>
                          )}
                          <div className="text-[10px] whitespace-pre-wrap" style={{ color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
                            {section.promptTemplate.substring(0, 200)}
                            {section.promptTemplate.length > 200 ? '...' : ''}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

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
                : 'Generate Preview'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Preview step
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
              Preview of generated perspective
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
              {creating ? 'Saving...' : 'Save Perspective'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Templates view (default)
  return (
    <div className="flex flex-col h-full" style={{ minHeight: 0 }}>
      <div className="flex-1 overflow-y-auto space-y-4 pt-2">
        {/* Perspectives (Artifacts) section */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <h4
              className="text-[11px] uppercase tracking-wide"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              Perspectives ({artifacts.length})
            </h4>
          </div>
          {artifacts.length === 0 ? (
            <div
              className="text-xs py-4 text-center"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              No perspectives yet. Create one from a template below.
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
                  <div style={{ color: 'var(--text-secondary)' }}>
                    {getArtifactIcon()}
                  </div>
                  <span
                    className="flex-1 truncate cursor-pointer"
                    style={{ color: 'var(--text-heading)' }}
                    title={artifact.filename}
                    onClick={() => {
                      if (onOpenDocument) {
                        onOpenDocument({
                          id: artifact.id,
                          filename: artifact.filename,
                          contentType: artifact.contentType,
                          sizeBytes: artifact.sizeBytes,
                          parseStatus: artifact.parseStatus,
                          docType: artifact.docType,
                          artifactConfig: artifact.artifactConfig,
                        });
                      }
                    }}
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
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Templates section */}
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
            {systemTemplates.length === 0 ? (
              <div
                className="text-xs py-2 text-center"
                style={{ color: 'var(--text-secondary-strong)' }}
              >
                No perspective templates available
              </div>
            ) : (
              <TemplatesList
                onTemplateSelect={handleTemplateSelect}
                templates={systemTemplates}
                groupedTemplates={groupedSystemTemplates}
              />
            )}
          </div>

          {/* Custom Templates */}
          {customTemplates.length > 0 && (
            <div>
              <div className="mb-2">
                <h4
                  className="text-[11px] uppercase tracking-wide"
                  style={{ color: 'var(--text-tertiary-weak)' }}
                >
                  Custom Templates ({customTemplates.length})
                </h4>
              </div>
              <TemplatesList
                onTemplateSelect={handleTemplateSelect}
                templates={customTemplates}
                groupedTemplates={groupedCustomTemplates}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default PerspectivesPanel;
