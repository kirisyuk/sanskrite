import { useMemo } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export type MergedItem = {
  type: 'message' | 'interaction';
  data: any;
  timestamp: number;
};

/**
 * Хук для подготовки объединенного списка сообщений и взаимодействий
 * @param excludeModelChat - если true, исключает interactions из Model вкладки (subkind='model.chat' или kind='ontology')
 */
export function useMergedMessages(excludeModelChat: boolean = false) {
  // Subscribe to lengths to trigger recalculation only when needed
  const interactionsLength = useGenaiChatStore((state) => state.interactions.length);
  const messagesLength = useGenaiChatStore((state) => state.messages.length);
  const streaming = useGenaiChatStore((state) => state.streaming);
  // Subscribe to _lastUpdate to detect when interactions or messages are updated (not just added)
  // This is critical for streaming chunks - messages content changes but length may not
  const lastUpdate = useGenaiChatStore((state) => (state as any)._lastUpdate || 0);
  
  // Get actual data using getState() inside useMemo to avoid stale closures
  const getState = useGenaiChatStore.getState;

  return useMemo(() => {
    const { interactions, messages: stateMessages, streaming: currentStreaming } = getState();
    
    // Skip Model chat filtering during streaming to save resources
    // During streaming, we show temporary messages from 'messages' array, not from 'interactions'
    // Filtering is only needed for persisted interactions, not for temporary streaming messages
    const shouldExcludeModelChat = excludeModelChat && !currentStreaming;
    
    const merged: MergedItem[] = [];

    // Add interactions first (user, assistant, and section markers)
    // This ensures interactions are processed before messages during streaming
    // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
    interactions
      .filter((it) => {
        // Exclude perspective interactions
        if (
          it.kind === 'note' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.')
        ) {
          return false;
        }
        // Exclude queued tasks interactions (they are metadata, not chat content)
        if (
          it.kind === 'note' &&
          typeof it.subkind === 'string' &&
          it.subkind === 'queued.tasks'
        ) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (
          it.kind === 'system' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.') &&
          it.subkind.endsWith('.analysis')
        ) {
          return false;
        }
        // Exclude document attachments (they are context, not chat content)
        if (it.subkind === 'document.attach') {
          return false;
        }
        // Exclude layout interactions (they are UI state, not chat content)
        if (it.kind === 'ontology' && it.subkind === 'layout') {
          return false;
        }
        // Exclude agent execution interactions (they are execution status, not chat content)
        if (it.kind === 'agent.execution') {
          return false;
        }
        // Exclude Model chat interactions when in Discuss view (skip during streaming to save resources)
        if (shouldExcludeModelChat) {
          // Exclude all interactions with subkind='model.chat' (created from Model tab)
          if (it.subkind === 'model.chat') {
            return false;
          }
          // Exclude ontology interactions (they belong to Model view)
          if (it.kind === 'ontology') {
            return false;
          }
        }
        // Include error interactions (subkind === 'error') - they should be displayed in chat
        if (it.kind === 'assistant' && it.subkind === 'error') {
          return true;
        }
        return (
          it.kind === 'user' ||
          it.kind === 'assistant' ||
          it.kind === 'action' ||
          it.kind === 'research' ||
          it.kind === 'section_start' ||
          it.kind === 'section_end'
        );
      })
      .forEach((it) => {
        let ts = 0;
        // First, try to get timestamp from originalMessageId (for messages created during streaming)
        // This ensures user and assistant messages from the same stream have consistent timestamps
        try {
          const originalMsgId = (it as any).originalMessageId;
          if (originalMsgId && typeof originalMsgId === 'string') {
            const parsedTs = parseInt(originalMsgId.split('-')[0]);
            if (!Number.isNaN(parsedTs) && parsedTs > 0) {
              ts = parsedTs;
            }
          }
        } catch {}
        // Fallback to createdAt if originalMessageId timestamp not available
        if (ts === 0) {
          try {
            const ca: any = (it as any).createdAt;
            if (ca && typeof ca.seconds === 'number') {
              ts = ca.seconds * 1000;
            } else if (typeof ca === 'string') {
              const d = Date.parse(ca);
              if (!Number.isNaN(d)) ts = d;
            } else if (typeof ca === 'number') {
              ts = ca;
            }
          } catch {}
        }
        merged.push({
          type: 'interaction',
          data: it,
          timestamp: ts,
        });
      });

    // Add filtered messages (only during streaming, and not already in interactions)
    // These are typically assistant messages that are being streamed
    if (streaming) {
      stateMessages.forEach((m: any) => {
        const hasInteraction = interactions.some(
          (it) =>
            (it as any).originalMessageId === m.id || it.id === m.id,
        );
        if (!hasInteraction) {
          // Parse timestamp from id (format: "timestamp-role")
          // Use Date.now() as fallback to ensure new messages appear at the end, not at the beginning
          const parsedTs = parseInt(m.id.split('-')[0]);
          const ts =
            !Number.isNaN(parsedTs) && parsedTs > 0 ? parsedTs : Date.now();
          merged.push({
            type: 'message',
            data: m,
            timestamp: ts,
          });
        }
      });
    }

    // Sort by timestamp; on ties, ensure user appears before assistant/other
    const priority = (item: MergedItem) => {
      try {
        if (item.type === 'interaction') {
          const k = String(item.data?.kind || '').toLowerCase();
          if (k === 'user') return 1;
          if (k === 'assistant') return 3;
          return 2; // section markers, action
        } else {
          const r = String(item.data?.role || '').toLowerCase();
          return r === 'user' ? 1 : 3;
        }
      } catch {
        return 2;
      }
    };
    merged.sort((a, b) => {
      if (a.timestamp !== b.timestamp) return a.timestamp - b.timestamp;
      return priority(a) - priority(b);
    });

    return merged;
  }, [
    interactionsLength,
    messagesLength,
    streaming,
    lastUpdate, // Include lastUpdate to trigger recalculation when interactions are updated
    excludeModelChat, // Include excludeModelChat to trigger recalculation when filter changes
  ]);
}

