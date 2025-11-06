import React from 'react';
import { api } from '../lib/api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTranslation } from 'react-i18next';
import { ThemeContext } from './ThemeProvider';
import { generatePerspectiveAnalysis } from '../lib/perspectiveAnalysis';

interface PerspectivesPanelProps {
  onPresetChange?: (preset: string) => void;
}

const PerspectivesPanel: React.FC<PerspectivesPanelProps> = ({ onPresetChange }) => {
  const { t, i18n } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const [preset, setPreset] = React.useState<string>('consulting');
  const [customQuery, setCustomQuery] = React.useState<string>('');
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [mdText, setMdText] = React.useState<string>('');
  const [runSeconds, setRunSeconds] = React.useState<number>(0);
  const runTimerRef = React.useRef<number | null>(null);

  // Cleanup timer on unmount to prevent memory leaks
  React.useEffect(() => {
    return () => {
      if (runTimerRef.current) {
        try {
          clearInterval(runTimerRef.current as any);
          runTimerRef.current = null;
        } catch {}
      }
    };
  }, []);
  const [saving, setSaving] = React.useState<boolean>(false);
  const [savedAt, setSavedAt] = React.useState<string | null>(null);
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const rafRef = React.useRef<number | null>(null);
  const [dropdownOpen, setDropdownOpen] = React.useState(false);
  const dropdownRef = React.useRef<HTMLDivElement | null>(null);
  const buttonRef = React.useRef<HTMLButtonElement | null>(null);
  const [dropdownPosition, setDropdownPosition] = React.useState<{ top: number; left: number; width: number } | null>(null);
  const genaiSettings = useGenAISettings();

  // PERFORMANCE FIX: Subscribe only to lengths, not entire arrays
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const messagesLength = useGenaiChatStore((s) => s.messages.length);
  const perspectives = useGenaiChatStore((s) => s.perspectives);
  const projectId = useGenaiChatStore((s) => s.projectId);
  
  // Get interactions/messages only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  const getMessages = React.useCallback(() => useGenaiChatStore.getState().messages, []);
  
  const prefLangCode = React.useMemo(() => {
    try {
      const raw = typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN';
      return (raw || 'EN').toUpperCase();
    } catch { return 'EN'; }
  }, []);

  const buildTranscript = React.useCallback(() => {
    const parts: string[] = [];
    // Use interactions first (they are persisted), then fallback to in-flight messages if streaming
    // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
    const interactions = getInteractions();
    interactions
      .filter((it: any) => {
        if (!it) return false;
        // Exclude perspective interactions
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        return it.kind === 'user' || it.kind === 'assistant';
      })
      .forEach((it: any) => {
        const isUser = it.kind === 'user';
        const text = String(it.content || '').trim();
        if (text) parts.push(`${isUser ? 'user' : 'assistant'}: ${text}`);
      });
    if (parts.length < 3) {
      const messages = getMessages();
      if (messages && messages.length > 0) {
        messages.forEach((m: any) => {
          const text = String(m?.content || '').trim();
          if (!text) return;
          const isUser = String(m?.role || '').toLowerCase() === 'user';
          parts.push(`${isUser ? 'user' : 'assistant'}: ${text}`);
        });
      }
    }
    const joined = parts.join('\n');
    // Analyze the entire chat transcript (no truncation)
    return joined;
  }, [getInteractions, getMessages, interactionsLength, messagesLength]);

  const presetToInstruction = React.useCallback((p: string) => {
    switch (p) {
      case 'themes':
        return `Task: Identify and cluster ALL themes discussed in the conversation and present the findings in an academic, neutral tone (no subjective praise/critique). Focus strictly on what is actually discussed; avoid speculation.

Output structure (Markdown, no code fences):
1) Summary (2–3 sentences): A neutral overview of what is being discussed overall.
2) Themes (as a list with clear subheadings). For EACH theme provide:
   - Title
   - Detailed_description: a thorough, structured explanation of the theme as if it were a short standalone article, capturing context, key points and nuances present in the conversation (no speculation beyond the transcript)
   - Trend: rising | stable | declining (with a one-line rationale grounded in the transcript)
   - Share: approximate share of the conversation focused on this theme (as a percentage, e.g., 15%)
3) Questions for further exploration: 3–5 concrete questions that require deeper coverage or clarification, based strictly on gaps/ambiguities in the transcript.

Constraints:
- Include ALL themes actually present in the transcript (no omissions).
- Academic style, concise, precise, grounded in the transcript.
- No strategy-level narratives beyond what is explicitly evidenced.
- Do not output JSON.`;
      case 'tasks':
        return `Extract actionable tasks from the conversation.
Return strictly JSON:
{"tasks": [{"title": "string", "assignees": ["string"], "status": "new|in_progress|blocked|done", "due"?: "ISO8601"}]}`;
      case 'sources':
        return `List sources referenced and their contributions.
Return strictly JSON:
{"sources": [{"name": "string", "url"?: "string", "contribution": "string", "weight": 0.0}]}`;
      case 'timeline':
        return `Build a timeline of key moments.
Return strictly JSON:
{"timeline": [{"ts": "ISO8601", "title": "string", "type": "decision|change|milestone|topic_shift", "details"?: "string"}]}`;
      case 'entities':
        return `Extract named entities and relations.
Return strictly JSON:
{"entities": [{"name": "string", "type": "person|group_of_persons|organization_or_platform|project_or_product|classifier_or_standard|classification_code|regulatory_document|government_agency|contract_document|location|other", "relations"?: [{"type": "string", "target": "string"}]}]}`;
      case 'risks_decisions':
        return `Task: Identify ALL decisions and risks discussed in the conversation and present the findings in an academic, neutral tone (no subjective praise/critique). Focus strictly on what is actually discussed; avoid speculation.

Output structure (Markdown, no code fences):
1) Summary (2–3 sentences): A neutral overview of the key decisions and risk areas present in the transcript.
2) Decisions (as a list with clear subheadings). For EACH decision provide:
   - Title
   - Detailed_description: a thorough, structured explanation of the decision as if it were a short standalone article, capturing context, options considered, arguments and intended outcome (no speculation beyond the transcript)
   - Status: proposed | accepted | rejected | unknown (with a one-line rationale grounded in the transcript)
   - Trend: rising | stable | declining (with a one-line rationale grounded in the transcript)
   - Share: approximate share of the conversation focused on this decision (as a percentage, e.g., 10%)
3) Risks (as a list with clear subheadings). For EACH risk provide:
   - Title
   - Detailed_description: a thorough, structured explanation of the risk as if it were a short standalone article, capturing context, drivers and potential impact (strictly from the transcript)
   - Severity: low | medium | high (with a one-line rationale grounded in the transcript)
   - Trend: rising | stable | declining (with a one-line rationale grounded in the transcript)
   - Share: approximate share of the conversation focused on this risk (as a percentage)
4) Questions for further exploration: 3–5 concrete questions that require deeper coverage or clarification, based strictly on gaps/ambiguities in the transcript.

Constraints:
- Include ALL decisions and risks actually present in the transcript (no omissions).
- Academic style, concise, precise, grounded in the transcript.
- No strategy-level narratives beyond what is explicitly evidenced.
- Do not output JSON.`;
      case 'discussion':
        return `Task: Analyze the discussion as a sequence of interactions that drive the group toward a shared understanding and eventual decisions. Identify who (humans and AI) contributed which moves, which techniques were used, and how effectively. Provide targeted recommendations at the end.

Output structure (Markdown, no code fences):
1) Summary (2–3 sentences): A neutral overview of the discussion’s purpose and trajectory.
2) Discussion Flow (chronological sections): For EACH key segment provide:
   - Segment title (what this part was about)
   - Purpose and outcome (what was attempted and achieved)
   - Participants and contributions (humans by role/name if available; AI if present)
   - Alignment/Disagreement (where consensus grew or diverged)
   - Blockers (ambiguities, missing info, conflicts)
3) Roles and Contributions: For EACH participant (humans and AI) provide:
   - Role (e.g., initiator, synthesizer, critic, decision-driver)
   - Exemplary contributions (quotes/paraphrases grounded in the transcript)
   - Influence assessment (low|medium|high) with one-line rationale
4) Techniques and Quality: Identify techniques used (e.g., clarification, decomposition, reframing, prioritization, hypothesis testing, referencing sources). For EACH technique provide:
   - Who used it
   - Quality (low|medium|high) with one-line rationale grounded in the transcript
5) Convergence and Decisions: What decisions (if any) were reached; level of consensus (low|medium|high); open questions blocking convergence.
6) Gaps and Risks: Key uncertainties, assumptions, and risk points revealed by the discussion.
7) Recommendations: 5–8 concrete recommendations to improve both the process and next-step outcomes (e.g., who should do what differently), grounded strictly in the transcript.

Constraints:
- Include ALL salient segments/roles/techniques present in the transcript (no omissions).
- Academic, neutral tone. Concise and precise. Evidence-grounded in the transcript.
- Do not output JSON.`;
      case 'consulting':
        return `ROLE: Senior Strategy Consultant with 15+ years experience across industries (McKinsey/BCG/Bain caliber). You've seen hundreds of strategic discussions and can spot patterns, gaps, and misframed problems instantly.

TASK: Analyze the preceding discussion from a professional consulting perspective. Provide balanced, actionable feedback that combines validation with constructive challenge.

CRITICAL: Your most valuable contribution is evaluating whether the team is asking the RIGHT question, not just answering their stated question well.

ANALYSIS FRAMEWORK:

1. FRAME ASSESSMENT (Most Important)
   - Is this the right problem to solve?
   - Are they addressing symptoms vs. root causes?
   - Is there a more strategic question one level up/down?
   - Suggest alternative framings if current one is suboptimal

2. WHAT'S DONE WELL
   - Specific strengths in their approach
   - Quality of analysis in certain areas
   - Good instincts or decisions to reinforce
   
3. CRITICAL GAPS
   - Missing perspectives (stakeholders, risks, metrics)
   - Unstated assumptions that should be tested
   - Organizational/political realities not addressed
   
4. ALTERNATIVE ANGLES
   - How would you approach this differently?
   - What framework or lens would add value?
   - What question should they ask that they haven't?

5. SPECIFIC NEXT QUESTIONS
   - 3–5 concrete questions to answer before moving forward
   - Prioritization: which matters most?
   - Quick wins vs. strategic moves

OUTPUT STRUCTURE (Markdown, no code fences):

FRAME CHECK
[Assess if they're solving the right problem. Suggest reframing if needed. This is your highest‑value contribution.]

WHAT'S STRONG
[2–3 specific validations. Be genuine — don't praise for the sake of it.]

WHERE TO DIG DEEPER  
[3–4 specific gaps or areas needing more thought. Concrete, not generic.]

ALTERNATIVE PERSPECTIVE
[How else could this be approached? What are they not seeing?]

KEY QUESTIONS FOR NEXT ROUND
[Specific, answerable questions. Not "develop a strategy" but "who owns budget authority for this?"]

BOTTOM LINE
[One paragraph: overall assessment + single most important next step]

TONE GUIDELINES:
- Experienced peer, not auditor
- Direct but respectful
- Specific examples > generic advice  
- "In my experience…" > "You must…"
- Acknowledge constraints and context
- Balance: ~40% validation, ~60% constructive challenge
- Avoid: buzzwords, platitudes, obvious statements
- Embrace: pattern recognition, experienced judgment, tough questions

AVOID:
- Saying "good discussion" without specifics
- Listing gaps without explaining why they matter
- Generic consulting speak ("synergize", "leverage core competencies")
- Being harsh without being helpful
- Ignoring what they did well

REMEMBER: The highest value is often in reframing the question itself, not just answering it better.

Constraints:
- Academic, neutral tone. Concise and precise. Evidence‑grounded in the transcript.
- Include ALL relevant points strictly supported by the transcript; avoid speculation.
- Do not output JSON.`;
      default:
        return '';
    }
  }, []);

  // Load existing perspective when preset or project changes
  React.useEffect(() => {
    if (!projectId || projectId === 'local-unfold') {
      setMdText('');
      return;
    }

    // Get perspective from cache (O(1) lookup)
    const existingPerspective = perspectives[preset];

    if (existingPerspective?.content) {
      setMdText(existingPerspective.content);
    } else {
      setMdText('');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, preset, perspectives]);

  // Notify parent when preset changes
  React.useEffect(() => {
    onPresetChange?.(preset);
  }, [preset, onPresetChange]);

  // Cleanup interval on unmount
  React.useEffect(() => {
    return () => {
      try {
        if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
      } catch {}
    };
  }, []);

  // Reset Perspectives on New Chat (any): project change or chat cleared
  React.useEffect(() => {
    try {
      if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
    } catch {}
    setPreset('consulting');
    setCustomQuery('');
    setMdText('');
    setError(null);
    setRunSeconds(0);
    setSaving(false);
    setSavedAt(null);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  React.useEffect(() => {
    if (interactionsLength === 0 && messagesLength === 0) {
      try {
        if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
      } catch {}
      setPreset('consulting');
      setCustomQuery('');
      setMdText('');
      setError(null);
      setRunSeconds(0);
      setSaving(false);
      setSavedAt(null);
    }
  }, [interactionsLength, messagesLength]);

  const ensureProject = useGenaiChatStore((s) => s.ensureProject);
  const persistedByLocalId = useGenaiChatStore((s) => (s as any).persistedByLocalId);

  const handleRun = React.useCallback(async (p: string, customInstruction?: string) => {
    setError(null);
    setMdText('');
    
    // Get project ID and ensure project exists
    let currentProjectId = projectId;
    if (!currentProjectId || currentProjectId === 'local-unfold') {
      try {
        currentProjectId = await ensureProject();
      } catch (e) {
        setError(t('unfold.perspectives.status.noProject'));
        return;
      }
    }

    // Delete old perspective of the same type before generating new one
    const perspectiveSubkind = `perspective.${p}`;
    const analysisSubkind = `perspective.${p}.analysis`;
    const oldPerspective = perspectives[p];

    // Also find and delete associated analysis
    // Need to handle both local and server IDs
    const map = persistedByLocalId || {};
    const perspectiveServerId = oldPerspective ? (map[oldPerspective.id] || oldPerspective.id) : null;
    const reverseMap: Record<string, string[]> = {};
    Object.entries(map).forEach(([local, server]) => {
      if (!reverseMap[server as string]) reverseMap[server as string] = [];
      reverseMap[server as string].push(local);
    });
    const perspectiveLocalIds = oldPerspective && perspectiveServerId ? (reverseMap[perspectiveServerId] || []) : [];
    
    const interactions = getInteractions();
    const oldAnalysis = oldPerspective ? interactions.find((it: any) => {
      if (it.kind !== 'system' || typeof it.subkind !== 'string' || it.subkind !== analysisSubkind) {
        return false;
      }
      // Check replyTo
      if (it.replyTo === oldPerspective.id || it.replyTo === perspectiveServerId || 
          perspectiveLocalIds.includes(it.replyTo || '')) {
        return true;
      }
      // Check relations
      if (it.relations && Array.isArray(it.relations)) {
        return it.relations.some((r: any) => 
          r.targetId === oldPerspective.id || r.targetId === perspectiveServerId ||
          perspectiveLocalIds.includes(r.targetId)
        );
      }
      return false;
    }) : null;

    if (oldPerspective && oldPerspective.id) {
      try {
        await api.deleteProjectInteraction(currentProjectId, oldPerspective.id);
        // Also remove from local state immediately
        useGenaiChatStore.setState((s) => {
          const updatedPerspectives = { ...s.perspectives };
          delete updatedPerspectives[p];
          return {
            interactions: s.interactions.filter((it) => it.id !== oldPerspective.id),
            perspectives: updatedPerspectives,
          };
        });
      } catch (e) {
        console.warn('[Perspectives] Failed to delete old perspective', e);
        // Continue anyway - we'll overwrite it
      }
    }

    // Delete associated analysis if it exists
    if (oldAnalysis && oldAnalysis.id) {
      try {
        await api.deleteProjectInteraction(currentProjectId, oldAnalysis.id);
        // Also remove from local state immediately
        useGenaiChatStore.setState((s) => ({
          interactions: s.interactions.filter((it) => it.id !== oldAnalysis.id)
        }));
      } catch (e) {
        console.warn('[Perspectives] Failed to delete old analysis', e);
        // Continue anyway
      }
    }

    // Start elapsed timer
    try {
      if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
    } catch {}
    setRunSeconds(0);
    
    // For custom perspective, use custom instruction
    let instruction: string;
    if (p === 'custom' && customInstruction) {
      instruction = `ROLE: Senior Strategy Consultant with 15+ years experience across industries (McKinsey/BCG/Bain caliber). You've seen hundreds of strategic discussions and can spot patterns, gaps, and misframed problems instantly.

TASK: Analyze the preceding discussion from a professional consulting perspective, adopting the specific viewpoint requested by the user. Provide balanced, actionable feedback that combines validation with constructive challenge.

USER'S REQUESTED PERSPECTIVE: ${customInstruction}

CRITICAL: Your task is to examine the discussion through the lens of "${customInstruction}". Adopt this perspective fully and comprehensively analyze how the topic and discussion appear from this alternative viewpoint.

ANALYSIS FRAMEWORK:

1. PERSPECTIVE FRAMING
   - Clearly articulate how "${customInstruction}" shapes the analysis
   - Explain what this perspective emphasizes or de-emphasizes compared to conventional views
   - Identify unique insights this perspective reveals

2. ANALYSIS FROM THIS PERSPECTIVE
   - How does the discussion look when viewed through "${customInstruction}"?
   - What patterns, gaps, or opportunities become visible from this angle?
   - What assumptions or framing decisions are revealed when adopting this viewpoint?

3. WHAT'S STRONG FROM THIS PERSPECTIVE
   - Specific strengths visible when viewed through "${customInstruction}"
   - Aspects of the discussion that align well with this perspective
   - Decisions or approaches that work particularly well from this angle

4. CRITICAL GAPS REVEALED
   - Missing considerations that become apparent from "${customInstruction}" viewpoint
   - Unstated assumptions that should be tested when adopting this perspective
   - Limitations or blind spots revealed by this alternative viewpoint

5. ALTERNATIVE INSIGHTS
   - What questions or considerations emerge specifically from "${customInstruction}"?
   - How would this perspective suggest approaching the topic differently?
   - What framework or lens does this perspective add?

6. SPECIFIC RECOMMENDATIONS
   - 3–5 concrete recommendations grounded in "${customInstruction}" perspective
   - Prioritization: which insights from this perspective matter most?
   - Actionable next steps informed by this viewpoint

OUTPUT STRUCTURE (Markdown, no code fences):

PERSPECTIVE FRAMING
[Articulate how "${customInstruction}" shapes your analysis. Explain what this perspective emphasizes and reveals.]

ANALYSIS FROM THIS VIEWPOINT
[Comprehensive analysis of the discussion as seen through "${customInstruction}". What patterns, insights, and considerations emerge?]

WHAT'S STRONG
[2–3 specific validations visible from this perspective. Be genuine and specific.]

WHERE TO DIG DEEPER
[3–4 specific gaps or areas needing more thought revealed by this perspective. Concrete, not generic.]

ALTERNATIVE INSIGHTS
[What unique insights does "${customInstruction}" reveal? What are they not seeing through this lens?]

KEY RECOMMENDATIONS
[Specific, actionable recommendations informed by "${customInstruction}" perspective. Prioritized and concrete.]

BOTTOM LINE
[One paragraph: overall assessment from "${customInstruction}" perspective + single most important next step]

TONE GUIDELINES:
- Experienced consultant adopting the requested perspective
- Direct but respectful
- Specific examples > generic advice
- "From the perspective of ${customInstruction}..." to ground insights
- Balance: ~40% validation, ~60% constructive challenge
- Avoid: buzzwords, platitudes, obvious statements
- Embrace: perspective-driven insights, alternative viewpoints, tough questions

Constraints:
- Academic, neutral tone. Concise and precise. Evidence-grounded in the transcript.
- Include ALL relevant points strictly supported by the transcript; avoid speculation.
- Do not output JSON.`;
    } else {
      instruction = presetToInstruction(p);
      if (!instruction) return;
    }
    const transcript = buildTranscript();
    if (!transcript) {
      setError(t('unfold.perspectives.status.noContext'));
      return;
    }
    // Build markdown-oriented prompt
    const langCode = (prefLangCode || 'EN').toLowerCase();
    const langName = (() => {
      switch (langCode) {
        case 'ru': return 'Russian';
        case 'en': return 'English';
        case 'kk':
        case 'kz': return 'Kazakh';
        default: return 'Russian';
      }
    })();
    const languageDirective = `You MUST produce the entire output in ${langName} language only (use other languages only for proper nouns/code identifiers). Do not switch languages.`;
    const prompt = `Preferred language: ${langName}. ${languageDirective}\n\nYou analyze a chat transcript and produce a clear, well-structured Markdown answer.\n\nTranscript (most recent last):\n${transcript}\n\nTask: ${instruction}\n\nOutput format: Markdown only (no code fences). Use headings, lists, and short paragraphs. Avoid JSON. Limit your total output to approximately 8 KB of text.`;
    setLoading(true);
    try {
      const combinedSystemInstruction = [languageDirective, (genaiSettings.systemInstruction || '').trim()].filter(Boolean).join('\n\n');
      // Kick off timer updates (1s cadence)
      const started = Date.now();
      try {
        runTimerRef.current = window.setInterval(() => {
          const s = Math.max(0, Math.floor((Date.now() - started) / 1000));
          setRunSeconds(s);
        }, 1000) as any;
      } catch {}
      const res = await api.genaiGenerate({
        model: genaiSettings.model,
        prompt,
        systemInstruction: combinedSystemInstruction || undefined,
        temperature: genaiSettings.temperature,
        // Approximate 8KB output cap
        maxOutputTokens: 8192,
        webSearchEnabled: !!genaiSettings.groundingEnabled,
        toolCallingEnabled: false,
        strictJSONEnabled: false,
      });
      const text = (res && typeof res.content === 'string') ? res.content : '';
      setMdText(text);

      // Save as Interaction with kind='note' and subkind='perspective.{preset}'
      if (text && currentProjectId && currentProjectId !== 'local-unfold') {
        try {
          const perspectiveSubkind = `perspective.${p}`;
          const savedInteraction = await api.createProjectInteraction(currentProjectId, {
            kind: 'note',
            subkind: perspectiveSubkind,
            content: text,
          });
          
          // Add to local state so it appears in interactions immediately
          const createdAt = new Date().toISOString();
          useGenaiChatStore.setState((s) => {
            const newInteraction: any = {
              id: savedInteraction.id,
              projectId: currentProjectId,
              kind: 'note',
              subkind: perspectiveSubkind,
              content: text,
              createdAt,
            };
            // Update perspectives cache
            const updatedPerspectives = { ...s.perspectives };
            updatedPerspectives[p] = {
              id: savedInteraction.id,
              content: text,
              preset: p,
              createdAt,
            };
            return {
              interactions: [...s.interactions, newInteraction],
              perspectives: updatedPerspectives,
            };
          });

          // Generate structured analysis after saving perspective
          (async () => {
            try {
              await generatePerspectiveAnalysis(savedInteraction.id, text, p);
            } catch (e) {
              console.warn('[Perspectives] Failed to generate analysis', e);
              // Don't block UI - analysis can be generated manually via refresh
            }
          })();
        } catch (e) {
          console.error('[Perspectives] Failed to save perspective interaction', e);
          // Don't show error to user - perspective is still displayed, just not persisted
        }
      }
    } catch (e: any) {
      setError(t('unfold.perspectives.status.analysisFailed'));
    } finally {
      setLoading(false);
      try {
        if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
      } catch {}
    }
    }, [buildTranscript, presetToInstruction, genaiSettings.model, genaiSettings.systemInstruction, genaiSettings.temperature, genaiSettings.maxOutputTokens, genaiSettings.groundingEnabled, t, prefLangCode, projectId, ensureProject, perspectives]);

  // Run only manually on selection (no auto reruns on chat updates)

  // Preset items with icons and i18n-powered descriptions (memoized with language dependency)
  const presetItems = React.useMemo(() => [
    {
      id: 'themes',
      title: t('unfold.selector.options.themes', { defaultValue: 'Themes' }),
      descr: t('unfold.perspectives.selector.options.themes', { defaultValue: 'Themes — Cluster all discussed topics; trend and share' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M4 7h16M4 12h16M4 17h16" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'consulting',
      title: t('unfold.selector.options.consulting', { defaultValue: 'Strategic Consulting Lens' }),
      descr: t('unfold.perspectives.selector.options.consulting', { defaultValue: 'Strategic Consulting Lens — Framing, gaps, angles, next questions' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <rect x="3" y="5" width="18" height="12" rx="2" strokeWidth="2" />
          <path d="M3 9h18" strokeWidth="2" />
          <path d="M7 17v2M17 17v2" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'discussion',
      title: t('unfold.selector.options.discussion', { defaultValue: 'Discussion Analysis' }),
      descr: t('unfold.perspectives.selector.options.discussion', { defaultValue: 'Discussion Analysis — Flow, roles, techniques, recommendations' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M4 6h10a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2H9l-5 3V8a2 2 0 0 1 2-2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      id: 'tasks',
      title: t('unfold.selector.options.tasks', { defaultValue: 'Tasks' }),
      descr: t('unfold.perspectives.selector.options.tasks', { defaultValue: 'Tasks — Action items, assignees, statuses, deadlines' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <rect x="4" y="4" width="6" height="6" rx="1" strokeWidth="2" />
          <path d="M5.5 7.5l1.5 1.5 2.5-2.5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M12 6h8M12 10h8M4 16h16" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'sources',
      title: t('unfold.selector.options.sources', { defaultValue: 'Sources' }),
      descr: t('unfold.perspectives.selector.options.sources', { defaultValue: 'Sources — Referenced sources and contributions' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M10.5 7.5l1.5-1.5a4.5 4.5 0 1 1 6.4 6.4l-1.5 1.5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M13.5 16.5l-1.5 1.5a4.5 4.5 0 1 1-6.4-6.4l1.5-1.5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      id: 'timeline',
      title: t('unfold.selector.options.timeline', { defaultValue: 'Timeline' }),
      descr: t('unfold.perspectives.selector.options.timeline', { defaultValue: 'Timeline — Milestones, topic shifts, decisions' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <circle cx="12" cy="12" r="9" strokeWidth="2" />
          <path d="M12 7v5l3 3" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'entities',
      title: t('unfold.selector.options.entities', { defaultValue: 'Entities' }),
      descr: t('unfold.perspectives.selector.options.entities', { defaultValue: 'Entities — Named entities and relations' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <circle cx="6" cy="12" r="2" strokeWidth="2" />
          <circle cx="18" cy="8" r="2" strokeWidth="2" />
          <circle cx="18" cy="16" r="2" strokeWidth="2" />
          <path d="M8 12h8M8 12l8-4M8 12l8 4" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'risks_decisions',
      title: t('unfold.selector.options.risksDecisions', { defaultValue: 'Risks & Decisions' }),
      descr: t('unfold.perspectives.selector.options.risksDecisions', { defaultValue: 'Risks & Decisions — Decisions and risk points' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M12 4l8 14H4l8-14z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M12 9v4" strokeWidth="2" strokeLinecap="round" />
          <path d="M12 17h.01" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'custom',
      title: t('unfold.selector.options.custom', { defaultValue: 'Your option' }),
      descr: t('unfold.perspectives.selector.options.custom', { defaultValue: 'Your option — Custom analytical perspective' }),
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
  ] as Array<{ id: string; title: string; descr: string; icon: React.ReactNode }>, [t, i18n.language]);

  // Auto-scroll to bottom on new content (like chat)
  React.useEffect(() => {
    if (scrollRef.current && (mdText || loading)) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [mdText, loading]);

  // Calculate dropdown position and close when clicking outside
  React.useEffect(() => {
    if (!dropdownOpen || !buttonRef.current) return;
    
    const updatePosition = () => {
      if (buttonRef.current) {
        const rect = buttonRef.current.getBoundingClientRect();
        setDropdownPosition({
          top: rect.top - 1, // 1px gap
          left: rect.left,
          width: rect.width,
        });
      }
    };
    
    updatePosition();
    window.addEventListener('scroll', updatePosition, true);
    window.addEventListener('resize', updatePosition);
    
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node) &&
          buttonRef.current && !buttonRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
        setDropdownPosition(null);
      }
    };
    
    document.addEventListener('mousedown', handleClickOutside);
    
    return () => {
      window.removeEventListener('scroll', updatePosition, true);
      window.removeEventListener('resize', updatePosition);
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [dropdownOpen]);

  const containerStyle: React.CSSProperties = React.useMemo(() => {
    // In dark theme with gradient border, don't set borderColor/backgroundImage
    if (theme === 'dark') {
      return {
        backgroundColor: 'var(--bg-input, #1A1A1A)',  /* Graphite black */
        boxShadow: 'none',
      };
    }
    
    // In light theme, use light background and border from CSS variables
    return {
      backgroundColor: 'var(--bg-input, #FFFFFF)',  /* White background for light theme */
      borderColor: 'var(--border-input, rgba(0, 0, 0, 0.15))',  /* Light gray border */
      boxShadow: '0 2px 10px rgba(0,0,0,0.06)',
    };
  }, [theme]);

  return (
    <div className="flex flex-col flex-1" style={{ minHeight: 0 }}>
      {/* Results area (scrollable, like chat) */}
      <div 
        ref={scrollRef} 
        className="flex-1 overflow-y-auto px-1 min-h-[160px]" 
        style={{ contentVisibility: 'auto', containIntrinsicSize: '800px 600px' }}
      >
        <div className="space-y-2 py-2" style={{ minHeight: '100%', display: 'flex', flexDirection: 'column' }}>
          {/* Empty state placeholder (when no document and no loading) */}
          {!loading && !mdText && (
            <div className="flex items-center justify-center" style={{ flex: 1, minHeight: '100%' }}>
              <div 
                className="max-w-md text-center px-6 py-8"
              >
                <div className="text-16" style={{ color: 'var(--text-heading)', lineHeight: '1.6', fontSize: 'clamp(18px, 2vw, 24px)', fontWeight: 500 }}>
                  {t('unfold.perspectives.emptyState.description')}
                </div>
                {t('unfold.perspectives.emptyState.note') && (
                  <div className="text-12" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.5', marginTop: '12px' }}>
                    {t('unfold.perspectives.emptyState.note')}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Skeleton loading (appears above input component) */}
          {loading && (
            <div className="space-y-2" style={{ flex: 'none' }}>
              <div style={{ height: 14, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 10, width: '90%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 10, width: '80%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
              <div style={{ height: 10, width: '70%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
              </div>
            </div>
          )}

          {/* Render markdown result (appears from bottom, like chat messages) */}
          {mdText && (
            <div className="space-y-2" style={{ flex: 'none' }}>
              {/* Article header with actions */}
              <div className="flex items-center justify-between mb-1">
                <div />
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    aria-label={t('unfold.perspectives.actions.copy')}
                    title={t('unfold.perspectives.actions.copy')}
                    onClick={async () => {
                      try {
                        if (!mdText) return;
                        if (navigator && 'clipboard' in navigator) {
                          await (navigator as any).clipboard.writeText(mdText);
                        } else {
                          const ta = document.createElement('textarea');
                          ta.value = mdText;
                          document.body.appendChild(ta);
                          ta.select();
                          document.execCommand('copy');
                          document.body.removeChild(ta);
                        }
                      } catch {}
                    }}
                    className="p-1 rounded"
                    style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <rect x="9" y="9" width="11" height="11" rx="2" strokeWidth="2" />
                      <rect x="4" y="4" width="11" height="11" rx="2" strokeWidth="2" />
                    </svg>
                  </button>
                  {savedAt && (
                    <span className="text-11" style={{ color: 'var(--text-secondary-strong)' }}>
                      {t('unfold.perspectives.actions.savedAt', { time: savedAt })}
                    </span>
                  )}
                  <button
                    type="button"
                    aria-label={t('unfold.perspectives.actions.save')}
                    title={t('unfold.perspectives.actions.save')}
                    onClick={async () => {
                      try {
                        if (!mdText) return;
                        setSaving(true); setSavedAt(null);
                        let pid = useGenaiChatStore.getState().projectId;
                        if (!pid || pid === 'local-unfold') {
                          try { pid = await (useGenaiChatStore.getState() as any).ensureProject?.(); } catch {}
                        }
                        if (!pid || typeof pid !== 'string') throw new Error('No project');
                        const fname = `perspective_${(preset || 'unknown')}.md`;
                        try {
                          const docs = await api.docsList(pid);
                          const existing = Array.isArray(docs) ? docs.find((d: any) => (d && (d.filename === fname))) : null;
                          if (existing && existing.id) {
                            await api.docsDelete(pid, existing.id);
                          }
                        } catch {}
                        await api.docsCreateFromText(pid, { filename: fname, contentType: 'text/markdown', text: mdText });
                        const dt = new Date();
                        setSavedAt(`${dt.getHours().toString().padStart(2,'0')}:${dt.getMinutes().toString().padStart(2,'0')}`);
                      } catch {
                      } finally {
                        setSaving(false);
                      }
                    }}
                    className="p-1 rounded"
                    style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: saving ? 'wait' : 'pointer' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                    disabled={saving}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M12 3v10" strokeWidth="2" strokeLinecap="round" />
                      <path d="M8 7l4-4 4 4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      <rect x="4" y="13" width="16" height="7" rx="2" strokeWidth="2" />
                    </svg>
                  </button>
                </div>
              </div>
              <div className="markdown text-13" style={{ color: 'var(--text-heading)' }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{mdText}</ReactMarkdown>
              </div>
              <div className="mt-3">
                <button
                  type="button"
                  className="text-12 px-2 py-1 rounded"
                  onClick={async () => {
                    try {
                      if (!mdText) return;
                      setSaving(true); setSavedAt(null);
                      let pid = useGenaiChatStore.getState().projectId;
                      if (!pid || pid === 'local-unfold') {
                        try { pid = await (useGenaiChatStore.getState() as any).ensureProject?.(); } catch {}
                      }
                      if (!pid || typeof pid !== 'string') throw new Error('No project');
                      const fname = `perspective_${(preset || 'unknown')}.md`;
                      try {
                        const docs = await api.docsList(pid);
                        const existing = Array.isArray(docs) ? docs.find((d: any) => (d && (d.filename === fname))) : null;
                        if (existing && existing.id) {
                          await api.docsDelete(pid, existing.id);
                        }
                      } catch {}
                      await api.docsCreateFromText(pid, { filename: fname, contentType: 'text/markdown', text: mdText });
                      const dt = new Date();
                      setSavedAt(`${dt.getHours().toString().padStart(2,'0')}:${dt.getMinutes().toString().padStart(2,'0')}`);
                    } catch {
                    } finally {
                      setSaving(false);
                    }
                  }}
                  disabled={saving}
                  style={{ background: 'var(--white-10)', color: 'var(--text-heading)', opacity: saving ? 0.6 : 1 }}
                >
                  <span className="inline-flex items-center gap-1">
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M12 3v10" strokeWidth="2" strokeLinecap="round" />
                      <path d="M8 7l4-4 4 4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      <rect x="4" y="13" width="16" height="7" rx="2" strokeWidth="2" />
                    </svg>
                    {t('unfold.perspectives.actions.saveToDocs')}
                  </span>
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Settings component at bottom (like UnfoldRequest) */}
        <div 
          className={`rounded-xl ${theme === 'dark' ? 'unfold-request-gradient-border' : 'border'} mt-2 relative`}
          style={{
            transition: 'all 0.2s ease',
            overflow: 'visible',
            ...(theme === 'dark' ? {
              // Dark theme: gradient border via CSS class
              backgroundColor: containerStyle.backgroundColor || 'var(--bg-input, #1A1A1A)',
              boxShadow: containerStyle.boxShadow,
            } : {
              // Light theme: regular border
              ...containerStyle,
            }),
          }}
        >
        <div className="p-3">
          {/* Label above dropdown */}
          <div className="text-12 mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            {t('unfold.perspectives.header.analyticalPerspective')}
          </div>
          {/* Dropdown selector on first line */}
          <div className="flex items-center gap-2 mb-2">
            <div className="relative" style={{ width: '33%', minWidth: 220, maxWidth: 360 }}>
              <button
                ref={buttonRef}
                id="perspective-select"
                type="button"
                onClick={() => setDropdownOpen(!dropdownOpen)}
                className="text-xs w-full text-left flex items-center justify-between cursor-pointer"
                style={{ 
                  background: 'transparent', 
                  color: 'var(--text-heading)', 
                  border: '1px solid var(--overlay-line-90)', 
                  borderRadius: 8, 
                  padding: '4px 8px',
                  outline: 'none',
                }}
              >
                <span>{presetItems.find(p => p.id === preset)?.title || t('unfold.perspectives.selector.placeholder')}</span>
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ transform: dropdownOpen ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s' }}>
                  <path d="M6 9l6 6 6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>
              {dropdownOpen && dropdownPosition && (
                <div
                  ref={dropdownRef}
                  className="fixed rounded-lg border shadow-lg overflow-hidden"
                  style={{
                    backgroundColor: 'var(--bg-sidebar)',
                    borderColor: 'var(--overlay-line-90)',
                    maxHeight: '300px',
                    overflowY: 'auto',
                    zIndex: 9999,
                    top: `${dropdownPosition.top}px`,
                    left: `${dropdownPosition.left}px`,
                    width: `${dropdownPosition.width}px`,
                    transform: 'translateY(-100%)',
                    marginTop: '-4px',
                  }}
                >
                  {presetItems.map((item) => (
                    <button
                      key={item.id}
                      type="button"
                      onClick={() => { 
                        setPreset(item.id); 
                        if (item.id !== 'custom') {
                          setCustomQuery('');
                        }
                        setDropdownOpen(false); 
                        setDropdownPosition(null); 
                      }}
                      className="w-full text-left px-3 py-2 text-12 transition-colors flex items-center gap-2"
                      style={{
                        color: item.id === preset ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                        background: item.id === preset ? 'var(--white-10)' : 'transparent',
                      }}
                      onMouseEnter={(e) => {
                        if (item.id !== preset) {
                          (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-06)';
                        }
                      }}
                      onMouseLeave={(e) => {
                        if (item.id !== preset) {
                          (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                        }
                      }}
                    >
                      <div style={{ color: 'var(--text-secondary-strong)' }}>{item.icon}</div>
                      <span>{item.title}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
          {/* Custom query input (shown when custom is selected) */}
          {preset === 'custom' && (
            <div className="mb-2">
              <input
                type="text"
                value={customQuery}
                onChange={(e) => setCustomQuery(e.target.value)}
                placeholder={t('unfold.perspectives.custom.placeholder', { defaultValue: 'Enter your analytical perspective...' })}
                className="text-12 px-2 py-1 rounded w-full"
                style={{ 
                  background: 'transparent', 
                  color: 'var(--text-heading)', 
                  border: 'none',
                  outline: 'none'
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && customQuery.trim() && !loading) {
                    e.preventDefault();
                    void handleRun('custom', customQuery.trim());
                  }
                }}
              />
            </div>
          )}
          {error && (
            <div className="text-12 mt-2" style={{ color: 'var(--danger-50)' }}>{error}</div>
          )}
          {loading && (
            <div className="mt-2 flex items-center gap-2 text-12" style={{ color: 'var(--text-secondary-strong)' }}>
              <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="12" r="9" strokeWidth="2" opacity="0.25" />
                <path d="M21 12a9 9 0 0 0-9-9" strokeWidth="2" strokeLinecap="round" />
              </svg>
              <span>{t('unfold.perspectives.status.running', { seconds: runSeconds })}</span>
            </div>
          )}
          {/* Bottom: controls row (like UnfoldRequest) */}
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.perspectives.header.choose')}</div>
              <span className="text-11 px-1.5 py-0.5 rounded" title={t('unfold.perspectives.header.langTitle')} style={{ color: 'var(--text-secondary-strong)', background: 'var(--white-06)' }}>{prefLangCode}</span>
              <span className="text-11 px-1.5 py-0.5 rounded" title={t('unfold.perspectives.header.presetTitle')} style={{ color: 'var(--text-secondary-strong)', background: 'var(--white-06)' }}>{preset || '—'}</span>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                className="p-1.5 rounded"
                title={t('unfold.perspectives.actions.refresh', { defaultValue: 'Refresh' })}
                onClick={() => { 
                  if (preset === 'custom' && customQuery.trim()) {
                    void handleRun(preset, customQuery.trim());
                  } else if (preset) {
                    void handleRun(preset);
                  }
                }}
                disabled={!preset || loading || (preset === 'custom' && !customQuery.trim())}
                style={{ background: 'transparent', color: 'var(--text-secondary-strong)', opacity: (!preset || loading || (preset === 'custom' && !customQuery.trim())) ? 0.6 : 1 }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path d="M20 7h-5M20 7v5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  <path d="M20 12a8 8 0 1 1-2.343-5.657" strokeWidth="2" strokeLinecap="round" />
                </svg>
              </button>
              <button
                type="button"
                aria-label={t('unfold.perspectives.actions.run')}
                className="flex items-center justify-center rounded-full"
                onClick={() => { 
                  if (preset === 'custom') {
                    if (customQuery.trim()) {
                      void handleRun(preset, customQuery.trim());
                    }
                  } else if (preset) {
                    void handleRun(preset);
                  }
                }}
                disabled={!preset || loading || (preset === 'custom' && !customQuery.trim())}
                style={{
                  width: '32px',
                  height: '32px',
                  backgroundColor: 'transparent',
                  opacity: (!preset || loading || (preset === 'custom' && !customQuery.trim())) ? 0.6 : 1,
                }}
                onMouseEnter={(e) => {
                  if (!(!preset || loading || (preset === 'custom' && !customQuery.trim()))) {
                    const svg = e.currentTarget.querySelector('svg path');
                    if (svg) {
                      (svg as SVGPathElement).style.fill = theme === 'light' ? '#FFB366' : '#FF9538';
                    }
                  }
                }}
                onMouseLeave={(e) => {
                  if (!(!preset || loading || (preset === 'custom' && !customQuery.trim()))) {
                    const svg = e.currentTarget.querySelector('svg path');
                    if (svg) {
                      (svg as SVGPathElement).style.fill = theme === 'dark' ? '#FF9538' : '#FFB366';
                    }
                  }
                }}
              >
                <svg className="w-6 h-6" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M8 5v14l11-7z" style={{ fill: theme === 'dark' ? '#FF9538' : '#FFB366' }} />
                </svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default PerspectivesPanel;


