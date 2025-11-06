import { useEffect, useRef, useState } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getAverageTiming } from '../lib/timingCache';
import { mapAliasToModel } from '../lib/modelMapping';

/**
 * Hook для управления таймерами чата
 */
export function useChatTimers() {
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  const genaiThinkingIntervalRef = useRef<number | null>(null);
  const [genaiThinkingSeconds, setGenaiThinkingSeconds] = useState(0);
  const [averageTimingSeconds, setAverageTimingSeconds] = useState<number | null>(null);

  // GenAI thinking seconds timer: runs until first chunk arrives
  useEffect(() => {
    const msgs = useGenaiChatStore.getState().messages;
    const last = msgs[msgs.length - 1];
    // Check if we're waiting for the first chunk: streaming is active, last message is assistant, and content is empty
    const awaitingFirstChunk = !!(genaiChatStreaming && last && last.role === 'assistant' && !last.content);
    
    if (awaitingFirstChunk) {
      // Start timer if not already running
      if (!genaiThinkingIntervalRef.current) {
        setGenaiThinkingSeconds(0);
        genaiThinkingIntervalRef.current = window.setInterval(() => {
          setGenaiThinkingSeconds((s) => s + 1);
        }, 1000) as unknown as number;
        
        // Fetch average timing estimate (without token count) - cached for 30 minutes
        const modelMapping = mapAliasToModel(genaiSettings.model);
        if (modelMapping) {
          getAverageTiming(modelMapping.provider, modelMapping.model)
            .then((timing) => {
              setAverageTimingSeconds(timing);
            })
            .catch((err) => {
              console.error('[useChatTimers] Failed to get average timing:', err);
              setAverageTimingSeconds(null);
            });
        } else {
          setAverageTimingSeconds(null);
        }
      }
    } else {
      // Stop timer when first chunk arrives or streaming stops
      if (genaiThinkingIntervalRef.current) {
        window.clearInterval(genaiThinkingIntervalRef.current);
        genaiThinkingIntervalRef.current = null;
        // Don't reset averageTimingSeconds here - keep it until next request
      }
    }
  }, [genaiChatStreaming, genaiChat.messages.length, genaiSettings.model]);

  // Cleanup genai thinking timer on unmount
  useEffect(() => {
    return () => {
      if (genaiThinkingIntervalRef.current) {
        window.clearInterval(genaiThinkingIntervalRef.current);
        genaiThinkingIntervalRef.current = null;
      }
    };
  }, []);

  return {
    genaiThinkingSeconds,
    averageTimingSeconds,
  };
}

