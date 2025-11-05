import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useProjectsStore } from '../stores/projectsStore';

// Track project IDs for which title generation is in progress to prevent duplicate requests
const generatingTitle = new Set<string>();

/**
 * Generates a project title based on conversation history.
 * @param projectId - ID of the project to generate title for
 * @param preferredLang - Target language ('ru', 'en', 'kk', etc.)
 * @returns Promise that resolves when title generation is complete
 */
export async function generateProjectTitle(params: {
  projectId: string;
  preferredLang: string;
}): Promise<void> {
  const { projectId, preferredLang } = params;

  // Skip if projectId is invalid
  if (!projectId || projectId === 'local-unfold') {
    console.log('[ProjectTitle] Skipping - invalid projectId', { projectId });
    return;
  }

  // Check if title generation is already in progress for this project
  if (generatingTitle.has(projectId)) {
    console.log('[ProjectTitle] Generation already in progress, skipping', { projectId });
    return;
  }

  // Check if project already has a proper name (not Draft or empty)
  try {
    // Fetch project directly from server to ensure we have latest data
    let projectData: any = null;
    try {
      projectData = await api.getProject(projectId);
      console.log('[ProjectTitle] Fetched project data', { projectId, displayName: projectData?.displayName });
    } catch (e) {
      console.warn('[ProjectTitle] Failed to fetch project, using currentProject', e);
      // Fallback to currentProject if fetch fails
      projectData = useProjectsStore.getState().currentProject;
    }

    const projectName = (projectData?.displayName || '').trim();
    if (projectName && projectName.toLowerCase() !== 'draft' && projectName.length > 0) {
      console.log('[ProjectTitle] Project already has a name, skipping', { projectId, projectName });
      return;
    }

    // Also check if title was already finalized for this project (localStorage check)
    if (typeof window !== 'undefined') {
      const finalized = window.localStorage.getItem(`chatTitleFinal:${projectId}`) === '1';
      if (finalized) {
        console.log('[ProjectTitle] Title already finalized, skipping', { projectId });
        return;
      }
    }
  } catch (e) {
    console.warn('[ProjectTitle] Failed to check project name', e);
  }

  // Mark as generating
  generatingTitle.add(projectId);

  try {
    const genaiChat = useGenaiChatStore.getState();
    
    // Build messages from interactions for better signal (use summary if available)
    const userInteractions = genaiChat.interactions.filter((it) => it.kind === 'user');
    const last5 = userInteractions.slice(Math.max(0, userInteractions.length - 5));
    const messages = last5.map((it) => ({
      role: 'user' as const,
      text: (it.summary && it.summary.trim()) ? it.summary : it.content,
    }));

    // If empty, fallback to chat messages
    if (messages.length === 0) {
      const chatMessages = genaiChat.messages.filter((m) => m.role === 'user');
      const fallbackLast5 = chatMessages.slice(Math.max(0, chatMessages.length - 5));
      fallbackLast5.forEach((m) => {
        messages.push({ role: 'user', text: m.content });
      });
    }

    // Skip if no messages
    if (messages.length === 0) {
      console.log('[ProjectTitle] No messages found, skipping', { projectId });
      generatingTitle.delete(projectId);
      return;
    }

    const langName = (() => {
      switch (preferredLang) {
        case 'ru': return 'Russian';
        case 'en': return 'English';
        case 'kk':
        case 'kz': return 'Kazakh';
        default: return 'Russian';
      }
    })();

    const transcript = messages.map((m) => `- ${m.role}: ${m.text}`).join('\n');

    const prompt = `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

You are generating a concise, descriptive title for a chat project based on its conversation.

Conversation transcript (most recent last):
${transcript}

Requirements:
- Title must be brief and descriptive (2-6 words)
- Use title case
- No quotes or special formatting
- Reflect the user's main intent or topic
- Be specific and non-generic

Return strictly JSON only, no markdown/code fences, matching this shape: { "title": "Your Title Here" }.

Example: {"title": "Climate Change Policies"}`;

    console.log('[ProjectTitle] Calling aiStructured', { projectId });
    const res = await api.aiStructured({
      prompt,
      minCapabilityScore: 6,
      maxOutputSize: 512,
    });
    console.log('[ProjectTitle] Received response from aiStructured', { 
      projectId, 
      ok: res?.ok,
      hasResult: !!res?.result,
      result: res?.result
    });

    // Parse response - aiStructured returns already parsed JSON in result field
    let parsed: any = null;
    if (res?.ok && res?.result && typeof res.result === 'object') {
      parsed = res.result;
      console.log('[ProjectTitle] Using result from aiStructured', { parsed });
    } else {
      console.warn('[ProjectTitle] aiStructured returned invalid response', { res });
      generatingTitle.delete(projectId);
      return;
    }

    if (!parsed || typeof parsed !== 'object' || !parsed.title || typeof parsed.title !== 'string') {
      console.log('[ProjectTitle] No valid title parsed, skipping', { parsed });
      generatingTitle.delete(projectId);
      return;
    }

    const title = parsed.title.trim();
    
    // Sanitize title: remove quotes, limit length
    const sanitizedTitle = title.replace(/^["']|["']$/g, '').trim();
    if (sanitizedTitle.length === 0 || sanitizedTitle.length > 80) {
      console.log('[ProjectTitle] Invalid title length, skipping', { sanitizedTitle });
      generatingTitle.delete(projectId);
      return;
    }

    // Verify projectId is still valid before updating
    // Fetch project again to ensure we have latest data
    let currentProject: any = null;
    try {
      currentProject = await api.getProject(projectId);
      console.log('[ProjectTitle] Verifying project before update', { projectId, displayName: currentProject?.displayName });
    } catch (e) {
      console.warn('[ProjectTitle] Failed to fetch project for verification, using currentProject', e);
      currentProject = useProjectsStore.getState().currentProject;
    }
    
    if (!currentProject || currentProject.id !== projectId) {
      console.warn('[ProjectTitle] Project changed during generation, aborting', { 
        expectedProjectId: projectId, 
        currentProjectId: currentProject?.id 
      });
      generatingTitle.delete(projectId);
      return;
    }

    // Update project with new title
    try {
      await api.updateProject(projectId, { 
        displayName: sanitizedTitle, 
        view: 'discovery' 
      } as any);
      
      console.log('[ProjectTitle] Successfully updated project', { projectId, title: sanitizedTitle });

      // Get updated project data from server to ensure consistency
      let updatedProject: any;
      try {
        updatedProject = await api.getProject(projectId);
      } catch {
        // Fallback to local update if fetch fails
        updatedProject = { ...currentProject, id: projectId, displayName: sanitizedTitle, view: 'discovery' };
      }

      // Update projects store
      useProjectsStore.setState((prev) => {
        const exists = prev.projects.some((p) => p.id === projectId);
        const updated = { ...updatedProject, id: projectId, displayName: sanitizedTitle, view: 'discovery' } as any;
        return {
          projects: exists
            ? prev.projects.map((p) => (p.id === projectId ? updated : p))
            : [updated, ...prev.projects],
          currentProject: updated,
        } as any;
      });

      // Refresh projects list from server to ensure consistency
      try {
        const { fetchProjects } = useProjectsStore.getState();
        await fetchProjects('discovery');
        console.log('[ProjectTitle] Refreshed projects list from server');
      } catch (e) {
        console.warn('[ProjectTitle] Failed to refresh projects list', e);
      }

      // Mark as finalized to prevent future suggestions
      if (typeof window !== 'undefined') {
        try {
          window.localStorage.setItem(`chatTitleFinal:${projectId}`, '1');
        } catch {}
      }
    } catch (e) {
      console.error('[ProjectTitle] Failed to update project', e);
    }
  } catch (e) {
    console.error('[ProjectTitle] Title generation failed', e);
  } finally {
    // Always remove from generating set
    generatingTitle.delete(projectId);
  }
}

