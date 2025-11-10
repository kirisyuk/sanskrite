import { useState, useMemo, useEffect, useCallback } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

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
    setAvailUserAnalysis,
    availLLMReview,
    setAvailLLMReview,
    availDiscussionFlow,
    setAvailDiscussionFlow,
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

