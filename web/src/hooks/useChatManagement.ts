import { useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { generateExperimentalResponse, ExperimentalResponse } from '../lib/experimentalResponse';
import { TextTask } from '../components/TextTasksPanel';

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
  const [experimentalResponse, setExperimentalResponse] = useState<ExperimentalResponse | null>(null);
  const [experimentalLoading, setExperimentalLoading] = useState(false);
  const lastUserMessageIdRef = useRef<string | null>(null);
  const [textTasks, setTextTasks] = useState<TextTask[]>([]);
  const [textTasksApplying, setTextTasksApplying] = useState(false);
  
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

  // Clear text tasks and experimental response when project changes
  useEffect(() => {
    setTextTasks([]);
    setTextTasksApplying(false);
    setExperimentalResponse(null);
    setExperimentalLoading(false);
    lastUserMessageIdRef.current = null;
  }, [projectId]);

  // Track new user messages and generate experimental response when panel is open
  useEffect(() => {
    const rightPanelType = null; // This should come from props or context
    if (rightPanelType !== 'experimental') return;
    
    const interactions = getInteractions();
    const userInteractions = interactions.filter(
      (it: any) => it.kind === 'user',
    );
    if (userInteractions.length === 0) return;
    
    const lastUserInteraction = userInteractions[userInteractions.length - 1];
    const messageId = lastUserInteraction.id;
    
    if (
      !messageId ||
      messageId === lastUserMessageIdRef.current ||
      !lastUserInteraction.content?.trim()
    ) {
      return;
    }
    
    lastUserMessageIdRef.current = messageId;
    setExperimentalLoading(true);
    
    generateExperimentalResponse(lastUserInteraction.content, messageId)
      .then((response) => {
        if (response) {
          setExperimentalResponse(response);
        }
      })
      .catch((error) => {
        console.error('[ExperimentalResponse] Failed to generate', error);
      })
      .finally(() => {
        setExperimentalLoading(false);
      });
  }, [userInteractionsCount, getInteractions]);

  const handleAddTextTask = useCallback((task: TextTask) => {
    setTextTasks((prev) => {
      if (prev.some((t) => t.id === task.id)) {
        return prev;
      }
      return [...prev, task];
    });
  }, []);

  const handleRemoveTextTask = useCallback((taskId: string) => {
    setTextTasks((prev) => prev.filter((t) => t.id !== taskId));
  }, []);

  const handleApplyTextTasks = useCallback(() => {
    if (textTasks.length === 0) return;
    setTextTasksApplying(true);
    try {
      const container = document.getElementById('unfold-request-container');
      if (container) {
        const textarea = container.querySelector(
          'textarea',
        ) as HTMLTextAreaElement;
        if (textarea) {
          const numberedList = textTasks
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
      setTextTasks([]);
    } catch (e) {
      console.warn('[DiscoveryPage] Failed to apply text tasks', e);
    } finally {
      setTextTasksApplying(false);
    }
  }, [textTasks]);

  return {
    experimentalResponse,
    setExperimentalResponse,
    experimentalLoading,
    setExperimentalLoading,
    textTasks,
    setTextTasks,
    textTasksApplying,
    handleAddTextTask,
    handleRemoveTextTask,
    handleApplyTextTasks,
  };
}

