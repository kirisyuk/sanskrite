import { useMemo } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

export type MergedItem = { 
  type: 'message' | 'interaction'; 
  data: any; 
  timestamp: number; 
};

/**
 * Hook для объединения messages и interactions с правильной сортировкой
 */
export function useChatMessages() {
  const genaiChat = useGenaiChatStore();
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);

  const mergedItems = useMemo(() => {
    const merged: MergedItem[] = [];

    // Add interactions first (user, assistant, and section markers)
    // This ensures interactions are processed before messages during streaming
    // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
    genaiChat.interactions
      .filter((it) => {
        // Exclude perspective interactions
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        return it.kind === 'user' || it.kind === 'assistant' || it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end';
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
        merged.push({ type: 'interaction', data: it, timestamp: ts });
      });

    // Add filtered messages (only during streaming, and not already in interactions)
    // These are typically assistant messages that are being streamed
    if (genaiChatStreaming) {
      genaiChat.messages.forEach((m) => {
        const hasInteraction = genaiChat.interactions.some((it) => 
          (it as any).originalMessageId === m.id || it.id === m.id
        );
        if (!hasInteraction) {
          // Parse timestamp from id (format: "timestamp-role")
          // Use Date.now() as fallback to ensure new messages appear at the end, not at the beginning
          const parsedTs = parseInt(m.id.split('-')[0]);
          const ts = (!Number.isNaN(parsedTs) && parsedTs > 0) ? parsedTs : Date.now();
          merged.push({ type: 'message', data: m, timestamp: ts });
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
      } catch { return 2; }
    };
    merged.sort((a, b) => {
      if (a.timestamp !== b.timestamp) return a.timestamp - b.timestamp;
      return priority(a) - priority(b);
    });

    return merged;
  }, [genaiChat.interactions, genaiChat.messages, genaiChatStreaming]);

  return mergedItems;
}

