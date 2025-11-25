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
    const { interactions, messages: stateMessages } = getState();
    
    // Debug logging for Model chat filtering
    if (excludeModelChat) {
      const modelChatInteractions = interactions.filter(it => it.subkind === 'model.chat' || it.kind === 'ontology');
      if (modelChatInteractions.length > 0) {
        console.log('[useMergedMessages] Found Model chat interactions to exclude', {
          excludeModelChat,
          modelChatCount: modelChatInteractions.length,
          interactions: modelChatInteractions.map(it => ({ id: it.id, kind: it.kind, subkind: it.subkind }))
        });
      }
    }
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
        // Exclude Model chat interactions when in Discuss view
        if (excludeModelChat) {
          // TEMPORARY: Exclude only user interactions with subkind='model.chat' (created from Model tab)
          // Assistant messages from Model should be visible in Discuss chat
          if (it.subkind === 'model.chat' && it.kind === 'user') {
            console.log('[useMergedMessages] Excluding Model chat user interaction', { id: it.id, kind: it.kind, subkind: it.subkind });
            return false;
          }
          // Exclude ontology interactions (they belong to Model view)
          if (it.kind === 'ontology') {
            console.log('[useMergedMessages] Excluding ontology interaction', { id: it.id, kind: it.kind, subkind: it.subkind });
            return false;
          }
          // Exclude user interactions that are likely from Model mode
          if (it.kind === 'user' && !it.subkind) {
            // Find the first ontology interaction in the project
            const ontologyInteractions = interactions.filter((other) => other.kind === 'ontology');
            if (ontologyInteractions.length > 0) {
              // Get the earliest ontology interaction time
              const firstOntologyTime = Math.min(
                ...ontologyInteractions.map((other) => {
                  try {
                    const ca: any = other.createdAt;
                    if (ca && typeof ca.seconds === 'number') {
                      return ca.seconds * 1000;
                    } else if (typeof ca === 'string') {
                      return Date.parse(ca) || 0;
                    } else if (typeof ca === 'number') {
                      return ca;
                    }
                  } catch {}
                  return 0;
                }).filter(t => t > 0)
              );
              
              // Get user interaction time
              const userTime = (() => {
                try {
                  const ca: any = it.createdAt;
                  if (ca && typeof ca.seconds === 'number') {
                    return ca.seconds * 1000;
                  } else if (typeof ca === 'string') {
                    return Date.parse(ca) || 0;
                  } else if (typeof ca === 'number') {
                    return ca;
                  }
                } catch {}
                return 0;
              })();
              
              // Exclude if user interaction was created after first ontology interaction
              // OR if it's followed by ontology interactions within 10 seconds
              if (userTime > 0 && firstOntologyTime > 0 && userTime >= firstOntologyTime) {
                console.log('[useMergedMessages] Excluding user interaction created after ontology interactions', { 
                  id: it.id, 
                  kind: it.kind, 
                  subkind: it.subkind,
                  userTime,
                  firstOntologyTime
                });
                return false;
              }
              
              // Also check if it's followed by ontology interactions (more precise check)
              const hasFollowingOntology = ontologyInteractions.some((other) => {
                const otherTime = (() => {
                  try {
                    const ca: any = other.createdAt;
                    if (ca && typeof ca.seconds === 'number') {
                      return ca.seconds * 1000;
                    } else if (typeof ca === 'string') {
                      return Date.parse(ca) || 0;
                    } else if (typeof ca === 'number') {
                      return ca;
                    }
                  } catch {}
                  return 0;
                })();
                // Consider it related if created within 10 seconds after user message
                return userTime > 0 && otherTime > userTime && otherTime - userTime < 10000;
              });
              
              if (hasFollowingOntology) {
                console.log('[useMergedMessages] Excluding user interaction followed by ontology', { id: it.id, kind: it.kind, subkind: it.subkind });
                return false;
              }
            }
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

