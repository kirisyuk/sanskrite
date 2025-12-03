import { useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { TextTask } from '../components/TextTasksPanel';
import type { QueuedTask } from '../types/queuedTasks';
import {
  loadQueuedTasks,
  addQueuedTask as addQueuedTaskAPI,
  removeQueuedTask as removeQueuedTaskAPI,
  markTasksApplied,
} from '../lib/queuedTasks';

export function useAnalysisManagement() {
  const [showUserAnalysis, setShowUserAnalysis] = useState(false);
  const [showLLMReview, setShowLLMReview] = useState(false);
  const [showDiscussionFlow, setShowDiscussionFlow] = useState(false);
  const [badgeUser, setBadgeUser] = useState(false);
  const [badgeLLM, setBadgeLLM] = useState(false);
  const [badgeFlow, setBadgeFlow] = useState(false);
  const [availUserAnalysis, setAvailUserAnalysis] = useState(false);
  const [availLLMReview, setAvailLLMReview] = useState(false);
  const [availDiscussionFlow, setAvailDiscussionFlow] = useState(false);
  const [riskBanner, setRiskBanner] = useState<null | {
    visible: boolean;
    level: 'High' | 'Medium' | 'Low';
    rationale: string;
  }>(null);
  const [isHighRisk, setIsHighRisk] = useState(false);
  const [expandedChip, setExpandedChip] = useState<string | null>(null);
  
  const genaiChat = useGenaiChatStore();
  const genaiChatInteractionsLength = useGenaiChatStore(
    (state) => state.interactions.length,
  );
  
  const getInteractions = useCallback(
    () => useGenaiChatStore.getState().interactions,
    [],
  );

  const hasAssistant = useMemo(() => {
    const interactions = getInteractions();
    return interactions.some(
      (it) => it.kind === 'assistant' && (it.contentSanitized || it.content),
    );
  }, [getInteractions, genaiChatInteractionsLength]);

  // Detect server-provided user stance (eval.stance linked to a user message)
  const stanceFromInteractionsTop = useMemo(() => {
    try {
      const interactions = getInteractions();
      const ints = [...interactions].reverse();
      const userIds = new Set(
        ints.filter((it) => it.kind === 'user').map((it) => it.id),
      );
      return ints.find(
        (it) =>
          it.subkind === 'eval.stance' && it.replyTo && userIds.has(it.replyTo),
      );
    } catch {
      return null as any;
    }
  }, [getInteractions, genaiChatInteractionsLength]);

  // If server already sent stance, make the User button available
  useEffect(() => {
    setAvailUserAnalysis(!!stanceFromInteractionsTop);
  }, [stanceFromInteractionsTop]);

  // Discussion Flow is always available now
  useEffect(() => {
    setAvailDiscussionFlow(true);
  }, []);

  // Callback for DiscussionFlow onFirstResult
  const onFirstResultDiscussionFlow = useCallback(() => {
    setBadgeFlow(false);
    setAvailDiscussionFlow(true);
  }, []);

  // Derive high-risk state from interactions
  useEffect(() => {
    try {
      const st = useGenaiChatStore.getState();
      const hi = [...st.interactions]
        .reverse()
        .find(
          (it) => (it as any).highRisk && (it as any).highRisk.level === 'High',
        );
      if (hi && (hi as any).highRisk) {
        const rationale = (hi as any).highRisk.rationale || '';
        setIsHighRisk(true as any);
        setRiskBanner((prev) =>
          prev
            ? { ...prev, rationale: rationale || prev.rationale || '' }
            : prev,
        );
      } else {
        setIsHighRisk(false as any);
      }
    } catch {}
  }, [genaiChat.interactions.length]);

  return {
    showUserAnalysis,
    setShowUserAnalysis,
    showLLMReview,
    setShowLLMReview,
    showDiscussionFlow,
    setShowDiscussionFlow,
    badgeUser,
    setBadgeUser,
    badgeLLM,
    setBadgeLLM,
    badgeFlow,
    setBadgeFlow,
    availUserAnalysis,
    availLLMReview,
    availDiscussionFlow,
    riskBanner,
    setRiskBanner,
    isHighRisk,
    setIsHighRisk,
    expandedChip,
    setExpandedChip,
    hasAssistant,
    stanceFromInteractionsTop,
    onFirstResultDiscussionFlow,
  };
}

export function useChatManagement() {
  const [queuedTasks, setQueuedTasks] = useState<QueuedTask[]>([]);
  const [textTasksApplying, setTextTasksApplying] = useState(false);
  const [tasksLoading, setTasksLoading] = useState(false);
  
  const genaiChat = useGenaiChatStore();
  const projectId = useGenaiChatStore((s) => s.projectId);
  const userInteractionsCount = useGenaiChatStore(
    (state) =>
      state.interactions.filter((it: any) => it.kind === 'user').length,
  );
  
  const getInteractions = useCallback(
    () => useGenaiChatStore.getState().interactions,
    [],
  );

  // Load queued tasks from interactions when project changes
  useEffect(() => {
    if (!projectId || projectId === 'local-unfold') {
      setQueuedTasks([]);
      setTextTasksApplying(false);
      return;
    }

    setTasksLoading(true);
    loadQueuedTasks(projectId)
      .then((tasks) => {
        setQueuedTasks(tasks);
      })
      .catch((e) => {
        console.warn('[useChatManagement] Failed to load queued tasks', e);
        setQueuedTasks([]);
      })
      .finally(() => {
        setTasksLoading(false);
      });

  }, [projectId]);

  // Sync queued tasks when interactions change (in case queued.tasks interaction was updated externally)
  const interactionsLength = useGenaiChatStore((state) => state.interactions.length);
  useEffect(() => {
    if (!projectId || projectId === 'local-unfold') return;
    
    const queuedTasksInteraction = genaiChat.interactions.find(
      (it) => it.kind === 'note' && it.subkind === 'queued.tasks'
    );
    
    if (queuedTasksInteraction) {
      try {
        const data = JSON.parse(queuedTasksInteraction.content);
        setQueuedTasks(data.tasks || []);
      } catch (e) {
        console.warn('[useChatManagement] Failed to parse queued tasks from interaction', e);
        setQueuedTasks([]);
      }
    } else {
      // If interaction doesn't exist, clear tasks (might have been deleted)
      // But only if we're not in the middle of loading
      if (!tasksLoading) {
        setQueuedTasks([]);
      }
    }
  }, [projectId, interactionsLength, genaiChat.interactions, tasksLoading]);


  const handleAddTextTask = useCallback(async (task: TextTask) => {
    if (!projectId || projectId === 'local-unfold') return;
    
    const queuedTask: QueuedTask = {
      id: task.id,
      type: 'text',
      title: task.title,
      prompt: task.prompt,
      createdAt: Date.now(),
      applied: false,
    };

    // Optimistic update - immediately update UI
    setQueuedTasks((prev) => {
      // Check if task already exists
      if (prev.some((t) => t.id === queuedTask.id)) {
        return prev;
      }
      return [...prev, queuedTask];
    });

    // Sync with server in background
    try {
      await addQueuedTaskAPI(projectId, queuedTask);
      // Update interactions in store to sync with server state
      await genaiChat.hydrateInteractions();
      // Reload tasks to ensure consistency (in case of conflicts)
      const updated = await loadQueuedTasks(projectId);
      setQueuedTasks(updated);
    } catch (e) {
      console.warn('[useChatManagement] Failed to add queued task', e);
      // Revert optimistic update on error
      setQueuedTasks((prev) => prev.filter((t) => t.id !== queuedTask.id));
    }
  }, [projectId, genaiChat]);

  const handleRemoveTextTask = useCallback(async (taskId: string) => {
    if (!projectId || projectId === 'local-unfold') return;
    
    // Store task for potential revert
    const taskToRemove = queuedTasks.find((t) => t.id === taskId);
    
    // Optimistic update - immediately update UI
    setQueuedTasks((prev) => prev.filter((t) => t.id !== taskId));

    // Sync with server in background
    try {
      await removeQueuedTaskAPI(projectId, taskId);
      // Update interactions in store to sync with server state
      await genaiChat.hydrateInteractions();
      // Reload tasks to ensure consistency (in case of conflicts)
      const updated = await loadQueuedTasks(projectId);
      setQueuedTasks(updated);
    } catch (e) {
      console.warn('[useChatManagement] Failed to remove queued task', e);
      // Revert optimistic update on error
      if (taskToRemove) {
        setQueuedTasks((prev) => {
          if (prev.some((t) => t.id === taskId)) {
            return prev; // Already exists
          }
          return [...prev, taskToRemove];
        });
      }
    }
  }, [projectId, genaiChat, queuedTasks]);

  const handleApplyTextTasks = useCallback(async () => {
    // Filter only unapplied text tasks
    const unappliedTextTasks = queuedTasks.filter(
      (t) => t.type === 'text' && !t.applied
    );
    
    if (unappliedTextTasks.length === 0) return;
    
    setTextTasksApplying(true);
    try {
      const container = document.getElementById('unfold-request-container');
      if (container) {
        const textarea = container.querySelector(
          'textarea',
        ) as HTMLTextAreaElement;
        if (textarea) {
          const numberedList = unappliedTextTasks
            .map((task, idx) => `${idx + 1}. ${task.prompt}`)
            .join('\n');
          
          const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
            window.HTMLTextAreaElement.prototype,
            'value',
          )?.set;
          if (nativeInputValueSetter) {
            nativeInputValueSetter.call(textarea, numberedList);
          } else {
            textarea.value = numberedList;
          }
          const inputEvent = new Event('input', { bubbles: true });
          textarea.dispatchEvent(inputEvent);
          textarea.focus();
          setTimeout(() => {
            textarea.setSelectionRange(
              textarea.value.length,
              textarea.value.length,
            );
          }, 0);
        }
      }
      
      // Mark tasks as applied
      if (projectId && projectId !== 'local-unfold') {
        const taskIds = unappliedTextTasks.map((t) => t.id);
        const now = Date.now();
        
        // Optimistic update - immediately update UI
        setQueuedTasks((prev) =>
          prev.map((t) =>
            taskIds.includes(t.id)
              ? { ...t, applied: true, appliedAt: now }
              : t
          )
        );

        // Sync with server in background
        try {
          await markTasksApplied(projectId, taskIds);
          // Update interactions in store to sync with server state
          await genaiChat.hydrateInteractions();
          // Reload tasks to ensure consistency (in case of conflicts)
          const updated = await loadQueuedTasks(projectId);
          setQueuedTasks(updated);
        } catch (e) {
          console.warn('[useChatManagement] Failed to mark tasks as applied', e);
          // Revert optimistic update on error
          setQueuedTasks((prev) =>
            prev.map((t) =>
              taskIds.includes(t.id)
                ? { ...t, applied: false, appliedAt: undefined }
                : t
            )
          );
        }
      }
    } catch (e) {
      console.warn('[useChatManagement] Failed to apply text tasks', e);
    } finally {
      setTextTasksApplying(false);
    }
  }, [queuedTasks, projectId, genaiChat]);

  // Handler for applying multiple tasks (used by PendingActionsPanel)
  const handleApplyTasks = useCallback(async (taskIds: string[]) => {
    if (taskIds.length === 0) return;
    
    const tasksToApply = queuedTasks.filter((t) => taskIds.includes(t.id));
    if (tasksToApply.length === 0) return;
    
    setTextTasksApplying(true);
    try {
      const container = document.getElementById('unfold-request-container');
      if (container) {
        const textarea = container.querySelector(
          'textarea',
        ) as HTMLTextAreaElement;
        if (textarea) {
          const numberedList = tasksToApply
            .map((task, idx) => `${idx + 1}. ${task.prompt}`)
            .join('\n');
          
          const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
            window.HTMLTextAreaElement.prototype,
            'value',
          )?.set;
          if (nativeInputValueSetter) {
            nativeInputValueSetter.call(textarea, numberedList);
          } else {
            textarea.value = numberedList;
          }
          const inputEvent = new Event('input', { bubbles: true });
          textarea.dispatchEvent(inputEvent);
          textarea.focus();
          setTimeout(() => {
            textarea.setSelectionRange(
              textarea.value.length,
              textarea.value.length,
            );
          }, 0);
        }
      }
      
      // Mark tasks as applied
      if (projectId && projectId !== 'local-unfold') {
        const now = Date.now();
        
        // Optimistic update - immediately update UI
        setQueuedTasks((prev) =>
          prev.map((t) =>
            taskIds.includes(t.id)
              ? { ...t, applied: true, appliedAt: now }
              : t
          )
        );

        // Sync with server in background
        try {
          await markTasksApplied(projectId, taskIds);
          // Update interactions in store to sync with server state
          await genaiChat.hydrateInteractions();
          // Reload tasks to ensure consistency (in case of conflicts)
          const updated = await loadQueuedTasks(projectId);
          setQueuedTasks(updated);
        } catch (e) {
          console.warn('[useChatManagement] Failed to mark tasks as applied', e);
          // Revert optimistic update on error
          setQueuedTasks((prev) =>
            prev.map((t) =>
              taskIds.includes(t.id)
                ? { ...t, applied: false, appliedAt: undefined }
                : t
            )
          );
        }
      }
    } catch (e) {
      console.warn('[useChatManagement] Failed to apply tasks', e);
    } finally {
      setTextTasksApplying(false);
    }
  }, [queuedTasks, projectId, genaiChat]);

  // Filter text tasks for backward compatibility
  const textTasks: TextTask[] = useMemo(() => {
    return queuedTasks
      .filter((t) => t.type === 'text')
      .map((t) => ({
        id: t.id,
        title: t.title,
        prompt: t.prompt,
      }));
  }, [queuedTasks]);

  return {
    queuedTasks,
    textTasks, // Filtered text tasks for backward compatibility
    textTasksApplying,
    tasksLoading,
    handleAddTextTask,
    handleRemoveTextTask,
    handleApplyTextTasks,
    handleApplyTasks, // New handler for applying multiple tasks
  };
}

