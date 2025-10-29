import React from 'react';
import { api } from '../lib/api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface PerspectivesPanelProps {
  // Placeholder for future filters/sections
}

const PerspectivesPanel: React.FC<PerspectivesPanelProps> = () => {
  const [preset, setPreset] = React.useState<string>('');
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [mdText, setMdText] = React.useState<string>('');
  const [runSeconds, setRunSeconds] = React.useState<number>(0);
  const runTimerRef = React.useRef<number | null>(null);
  const selectRef = React.useRef<HTMLSelectElement | null>(null);
  const [saving, setSaving] = React.useState<boolean>(false);
  const [savedAt, setSavedAt] = React.useState<string | null>(null);
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const rafRef = React.useRef<number | null>(null);
  const [showToTop, setShowToTop] = React.useState(false);
  const [showToBottom, setShowToBottom] = React.useState(false);
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();

  const interactions = useGenaiChatStore((s) => s.interactions);
  const messages = useGenaiChatStore((s) => s.messages);
  const prefLangCode = React.useMemo(() => {
    try {
      const raw = typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN';
      return (raw || 'EN').toUpperCase();
    } catch { return 'EN'; }
  }, []);

  const buildTranscript = React.useCallback(() => {
    const parts: string[] = [];
    // Use interactions first (they are persisted), then fallback to in-flight messages if streaming
    interactions
      .filter((it: any) => it && (it.kind === 'user' || it.kind === 'assistant'))
      .forEach((it: any) => {
        const isUser = it.kind === 'user';
        const text = String(it.content || '').trim();
        if (text) parts.push(`${isUser ? 'user' : 'assistant'}: ${text}`);
      });
    if (parts.length < 3 && messages && messages.length > 0) {
      messages.forEach((m: any) => {
        const text = String(m?.content || '').trim();
        if (!text) return;
        const isUser = String(m?.role || '').toLowerCase() === 'user';
        parts.push(`${isUser ? 'user' : 'assistant'}: ${text}`);
      });
    }
    const joined = parts.join('\n');
    // Analyze the entire chat transcript (no truncation)
    return joined;
  }, [interactions, messages]);

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

  // Reset Perspectives on New Chat (any): project change or chat cleared
  React.useEffect(() => {
    try {
      if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
    } catch {}
    setPreset('');
    setMdText('');
    setError(null);
    setRunSeconds(0);
    setSaving(false);
    setSavedAt(null);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [genaiChat.projectId]);

  React.useEffect(() => {
    const ints = Array.isArray(interactions) ? interactions.length : 0;
    const msgs = Array.isArray(messages) ? messages.length : 0;
    if (ints === 0 && msgs === 0) {
      try {
        if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
      } catch {}
      setPreset('');
      setMdText('');
      setError(null);
      setRunSeconds(0);
      setSaving(false);
      setSavedAt(null);
    }
  }, [interactions, messages]);

  const handleRun = React.useCallback(async (p: string) => {
    setError(null);
    setMdText('');
    // Start elapsed timer
    try {
      if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
    } catch {}
    setRunSeconds(0);
    const instruction = presetToInstruction(p);
    if (!instruction) return;
    const transcript = buildTranscript();
    if (!transcript) {
      setError('Нет контекста для анализа');
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
    } catch (e: any) {
      setError('Ошибка анализа');
    } finally {
      setLoading(false);
      try {
        if (runTimerRef.current) { clearInterval(runTimerRef.current as any); runTimerRef.current = null; }
      } catch {}
    }
  }, [buildTranscript, presetToInstruction, genaiSettings.model, genaiSettings.systemInstruction, genaiSettings.temperature, genaiSettings.maxOutputTokens, genaiSettings.groundingEnabled]);

  // Run only manually on selection (no auto reruns on chat updates)

  // Preset items with icons and short English descriptions
  const presetItems = [
    {
      id: 'themes',
      title: 'Themes',
      descr: 'Cluster all discussed topics with detailed descriptions, trend and share.',
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M4 7h16M4 12h16M4 17h16" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'consulting',
      title: 'Strategic Consulting Lens',
      descr: 'Senior consultant view: framing, gaps, alt angles, next questions.',
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
      title: 'Discussion Analysis',
      descr: 'Flow, roles (humans/AI), techniques quality, recommendations.',
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M4 6h10a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2H9l-5 3V8a2 2 0 0 1 2-2z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      id: 'tasks',
      title: 'Tasks',
      descr: 'Action items, assignees, statuses and deadlines derived from discussion.',
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
      title: 'Sources',
      descr: 'Referenced sources and their contribution to outcomes.',
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M10.5 7.5l1.5-1.5a4.5 4.5 0 1 1 6.4 6.4l-1.5 1.5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M13.5 16.5l-1.5 1.5a4.5 4.5 0 1 1-6.4-6.4l1.5-1.5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      ),
    },
    {
      id: 'timeline',
      title: 'Timeline',
      descr: 'Key milestones, topic shifts and decisions over time.',
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <circle cx="12" cy="12" r="9" strokeWidth="2" />
          <path d="M12 7v5l3 3" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
    {
      id: 'entities',
      title: 'Entities',
      descr: 'Named entities and relations discussed.',
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
      title: 'Risks & Decisions',
      descr: 'Decisions and risk points explicitly present in the transcript.',
      icon: (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M12 4l8 14H4l8-14z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M12 9v4" strokeWidth="2" strokeLinecap="round" />
          <path d="M12 17h.01" strokeWidth="2" strokeLinecap="round" />
        </svg>
      ),
    },
  ] as Array<{ id: string; title: string; descr: string; icon: React.ReactNode }>;

  // Scroll listener to toggle top/bottom helpers
  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
      rafRef.current = requestAnimationFrame(() => {
        const top = el.scrollTop;
        const bottomGap = el.scrollHeight - (el.scrollTop + el.clientHeight);
        setShowToTop(top > 100);
        setShowToBottom(bottomGap > 100);
      });
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    // Initialize state
    onScroll();
    return () => {
      el.removeEventListener('scroll', onScroll as any);
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    };
  }, [scrollRef]);

  return (
    <div ref={scrollRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]" style={{ contentVisibility: 'auto', containIntrinsicSize: '800px 600px', position: 'relative' }}>
      {/* Selector component */}
      <div className="w-full mb-2" style={{ position: 'sticky', top: 0, zIndex: 5, background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)', borderRadius: 12, padding: 12 }}>
        {/* Header: title + language + actions */}
        <div className="flex items-center justify-between gap-2 mb-2">
          <div className="flex items-center gap-2">
            <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Choose perspective</div>
            <span className="text-11 px-1.5 py-0.5 rounded" title="Preferred language for answers" style={{ color: 'var(--text-secondary-strong)', background: 'var(--white-06)' }}>{prefLangCode}</span>
            <span className="text-11 px-1.5 py-0.5 rounded" title="Selected preset id" style={{ color: 'var(--text-secondary-strong)', background: 'var(--white-06)' }}>{preset || '—'}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="p-1.5 rounded"
              title="Refresh"
              onClick={() => { const val = selectRef.current?.value || preset; if (val) void handleRun(val); }}
              disabled={!preset || loading}
              style={{ background: 'transparent', color: 'var(--text-secondary-strong)', opacity: (!preset || loading) ? 0.6 : 1 }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M20 7h-5M20 7v5" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M20 12a8 8 0 1 1-2.343-5.657" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        </div>
        {/* Description */}
        <div className="text-12 mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
          View the discussion from different analytical angles: clustered themes, tasks, sources, timeline, entities, and risks/decisions. Choose one to generate an academic, structured summary in your preferred language.
        </div>
        {/* Compact dropdown selector with strict icon and Run on the right */}
        <div className="flex items-center gap-2">
          {/* Selected icon (strict stroke, no fill, no background) */}
          <div className="p-1 rounded" style={{ color: 'var(--text-secondary-strong)' }}>
            {(presetItems.find(p => p.id === preset)?.icon) || (
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="12" r="9" strokeWidth="2" />
              </svg>
            )}
          </div>
          <label htmlFor="perspective-select" className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Select</label>
          <select
            id="perspective-select"
            ref={selectRef}
            value={preset}
            onChange={(e) => setPreset(e.target.value)}
            className="text-12 px-2 py-1 rounded"
            style={{ background: 'var(--bg-elevated)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)', width: '33%', minWidth: 220, maxWidth: 360 }}
          >
            <option value="" disabled>— Choose an analytical view —</option>
            <option value="consulting">Strategic Consulting Lens — Framing, gaps, angles, next questions</option>
            <option value="discussion">Discussion Analysis — Flow, roles, techniques, recommendations</option>
            <option value="themes">Themes — Cluster all discussed topics; trend and share</option>
            <option value="tasks">Tasks — Action items, assignees, statuses, deadlines</option>
            <option value="sources">Sources — Referenced sources and contributions</option>
            <option value="timeline">Timeline — Milestones, topic shifts, decisions</option>
            <option value="entities">Entities — Named entities and relations</option>
            <option value="risks_decisions">Risks & Decisions — Decisions and risk points</option>
          </select>
          <button
            type="button"
            className="text-12 px-2 py-1 rounded"
            onClick={() => { const val = selectRef.current?.value || preset; if (val) void handleRun(val); }}
            disabled={!preset || loading}
            style={{ background: 'var(--white-10)', color: 'var(--text-heading)', opacity: (!preset || loading) ? 0.6 : 1 }}
          >
            Run
          </button>
        </div>
        {error && (
          <div className="text-12 mt-2" style={{ color: 'var(--danger-50)' }}>{error}</div>
        )}
      </div>

      {/* Running status below sticky selector */}
      {loading && (
        <div className="mt-2 flex items-center gap-2 text-12" style={{ color: 'var(--text-secondary-strong)' }}>
          <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <circle cx="12" cy="12" r="9" strokeWidth="2" opacity="0.25" />
            <path d="M21 12a9 9 0 0 0-9-9" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <span>Running… {runSeconds}s</span>
        </div>
      )}

      {/* Scroll helpers */}
      {showToTop && (
        <button
          type="button"
          aria-label="Scroll to top"
          title="Scroll to top"
          onClick={() => { const el = scrollRef.current; if (el) el.scrollTo({ top: 0, behavior: 'smooth' }); }}
          className="p-1 rounded"
          style={{ position: 'sticky', top: 6, float: 'right', background: 'transparent', color: 'var(--text-secondary-strong)' }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M12 5l-6 6M12 5l6 6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M12 5v14" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </button>
      )}

      {/* Render markdown result below the component */}
      {loading && (
        <div className="mt-2 space-y-2">
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
      {mdText && (
        <>
          {/* Article header with Save icon */}
          <div className="flex items-center justify-between mb-1">
            <div />
            <div className="flex items-center gap-2">
              <button
                type="button"
                aria-label="Copy"
                title="Copy"
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
                  Saved {savedAt}
                </span>
              )}
              <button
                type="button"
                aria-label="Save"
                title="Save"
                onClick={async () => {
                  try {
                    if (!mdText) return;
                    setSaving(true); setSavedAt(null);
                    let pid = useGenaiChatStore.getState().projectId;
                    if (!pid || pid === 'local-unfold') {
                      try { pid = await (useGenaiChatStore.getState() as any).ensureProject?.(); } catch {}
                    }
                    if (!pid || typeof pid !== 'string') throw new Error('No project');
                    const fname = `perspective_${(selectRef.current?.value || preset || 'unknown')}.md`;
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
                  const fname = `perspective_${(selectRef.current?.value || preset || 'unknown')}.md`;
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
                Save to documents
              </span>
            </button>
          </div>
        </>
      )}

      {showToBottom && (
        <div style={{ height: 0 }} />
      )}

      {showToBottom && (
        <button
          type="button"
          aria-label="Scroll to bottom"
          title="Scroll to bottom"
          onClick={() => { const el = scrollRef.current; if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' }); }}
          className="p-1 rounded"
          style={{ position: 'sticky', bottom: 6, float: 'right', background: 'transparent', color: 'var(--text-secondary-strong)', marginTop: 6 }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <path d="M12 19l-6-6M12 19l6-6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M12 5v14" strokeWidth="2" strokeLinecap="round" />
          </svg>
        </button>
      )}
      {!preset && !loading && !mdText && !error && null}
    </div>
  );
};

export default PerspectivesPanel;


