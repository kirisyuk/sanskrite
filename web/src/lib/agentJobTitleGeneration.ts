import { api } from './api';

// Track job IDs for which title generation is in progress to prevent duplicate requests
const generatingTitle = new Set<string>();

/**
 * Generates a title for an agent job based on the user query.
 * @param jobId - ID of the job to generate title for
 * @param userQuery - User query/task description
 * @param preferredLang - Target language ('ru', 'en', 'kk', etc.)
 * @returns Promise that resolves with the generated title
 */
export async function generateAgentJobTitle(params: {
  jobId: string;
  userQuery: string;
  preferredLang: string;
}): Promise<string | null> {
  const { jobId, userQuery, preferredLang } = params;

  // Skip if jobId is invalid or userQuery is empty
  if (!jobId || !userQuery || userQuery.trim().length === 0) {
    console.log('[AgentJobTitle] Skipping - invalid jobId or empty userQuery', { jobId, hasQuery: !!userQuery });
    return null;
  }

  // Check if title generation is already in progress for this job
  if (generatingTitle.has(jobId)) {
    console.log('[AgentJobTitle] Generation already in progress, skipping', { jobId });
    return null;
  }

  // Mark as generating
  generatingTitle.add(jobId);

  try {
    const langName = (() => {
      switch (preferredLang) {
        case 'ru': return 'Russian';
        case 'en': return 'English';
        case 'kk':
        case 'kz': return 'Kazakh';
        default: return 'Russian';
      }
    })();

    // Use the same fast model as project title generation
    const { useGenAISettings } = await import('../stores/genaiSettingsStore');
    const settings = useGenAISettings.getState();
    const evalModel = (settings.stanceModel && settings.stanceModel.trim()) ? settings.stanceModel : 'gemini_fast';

    const schema = {
      type: 'object',
      additionalProperties: false,
      properties: {
        title: { type: 'string', minLength: 1, maxLength: 80 }
      },
      required: ['title']
    } as any;

    const prompt = `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

You are generating a concise, descriptive title for an agent job based on the user's task.

User task:
${userQuery}

Requirements:
- Title must be brief and descriptive (2-6 words)
- Use title case
- No quotes or special formatting
- Reflect the user's main intent or task
- Be specific and non-generic

Return strictly JSON only, no markdown/code fences, matching this shape: { "title": "Your Title Here" }.

Example: {"title": "Document Analysis and Preparation"}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    console.log('[AgentJobTitle] Calling genaiGenerate with fast model', { jobId, model: evalModel });
    const res = await api.genaiGenerate({
      model: evalModel,
      prompt,
      strictJSONEnabled: true,
      jsonSchema: schema,
      maxOutputTokens: 512,
      webSearchEnabled: false,
      systemInstruction: langDirective,
    });
    console.log('[AgentJobTitle] Received response from genaiGenerate', { 
      jobId, 
      hasContent: !!res?.content,
      hasRawJSON: !!(res as any)?.rawJSON
    });

    // Helper function to fix common JSON escaping issues
    const fixJSONEscaping = (jsonStr: string): string => {
      return jsonStr.replace(/\\'/g, "'");
    };

    // Parse response - same approach as project title generation
    let parsed: any = null;
    try {
      if (typeof res?.content === 'string') {
        const s = res.content.trim();
        if (s.startsWith('{') || s.startsWith('[')) {
          const fixed = fixJSONEscaping(s);
          parsed = JSON.parse(fixed);
        }
      }
      const rawJSON = (res as any)?.rawJSON;
      if (!parsed && typeof rawJSON === 'string') {
        const s2 = rawJSON.trim();
        if (s2.startsWith('{') || s2.startsWith('[')) {
          const fixed = fixJSONEscaping(s2);
          parsed = JSON.parse(fixed);
        }
      }
      if (!parsed && typeof res?.content === 'string') {
        const m = res.content.match(/\{[\s\S]*\}/);
        if (m && m[0]) {
          const fixed = fixJSONEscaping(m[0]);
          parsed = JSON.parse(fixed);
        }
      }
    } catch (pe) {
      console.warn('[AgentJobTitle] Parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || !parsed.title || typeof parsed.title !== 'string') {
      console.warn('[AgentJobTitle] No valid title parsed, skipping', { parsed });
      generatingTitle.delete(jobId);
      return null;
    }

    const title = parsed.title.trim();
    
    // Sanitize title: remove quotes, limit length
    const sanitizedTitle = title.replace(/^["']|["']$/g, '').trim();
    if (sanitizedTitle.length === 0 || sanitizedTitle.length > 80) {
      console.log('[AgentJobTitle] Invalid title length, skipping', { sanitizedTitle });
      generatingTitle.delete(jobId);
      return null;
    }

    console.log('[AgentJobTitle] Successfully generated title', { jobId, title: sanitizedTitle });
    return sanitizedTitle;
  } catch (e) {
    console.error('[AgentJobTitle] Title generation failed', e);
    return null;
  } finally {
    // Always remove from generating set
    generatingTitle.delete(jobId);
  }
}

