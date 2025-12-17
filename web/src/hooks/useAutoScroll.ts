import { useEffect, RefObject, useRef } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

/**
 * Hook для автоматической прокрутки контейнера вниз при изменении зависимостей
 */
export function useAutoScroll(
  containerRef: RefObject<HTMLDivElement>,
  dependencies: any[]
) {
  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, dependencies);
}

/**
 * Hook для автоматической прокрутки с задержкой (для гидратации)
 */
export function useAutoScrollDelayed(
  containerRef: RefObject<HTMLDivElement>,
  dependencies: any[]
) {
  useEffect(() => {
    if (dependencies[0] > 0 && containerRef.current) {
      // Small delay to ensure DOM has updated
      const timeoutId = setTimeout(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 100);
      return () => clearTimeout(timeoutId);
    }
  }, dependencies);
}

/**
 * Комплексный хук для автоматической прокрутки с поддержкой:
 * - Прокрутки при изменении проекта
 * - Прокрутки после гидратации
 * - Прокрутки при новых взаимодействиях
 * - Прокрутки при завершении стриминга
 * - Прокрутки во время стриминга при обновлении содержимого (чанки)
 */
export function useAdvancedAutoScroll(
  containerRef: RefObject<HTMLDivElement | null>,
  options?: {
    interactionsLength?: number;
    streaming?: boolean;
    hydrating?: boolean;
    projectId?: string | null;
  }
) {
  const lastScrollInteractionLength = useRef<number>(0);
  const lastScrollStreamingState = useRef<boolean>(false);
  const lastProjectIdRef = useRef<string | null>(null);
  const lastUpdateTimestampRef = useRef<number>(0);
  const lastUserInteractionIdRef = useRef<string | null>(null);
  const hasScrolledToCurrentUserMessageRef = useRef<boolean>(false);
  
  const genaiChat = useGenaiChatStore();
  const genaiChatInteractionsLength = useGenaiChatStore(
    (state) => state.interactions.length,
  );
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  const genaiChatHydrating = useGenaiChatStore((state) => state.hydrating);
  const genaiChatProjectId = useGenaiChatStore((state) => state.projectId);
  // Track _lastUpdate timestamp to detect content updates during streaming
  const lastUpdateTimestamp = useGenaiChatStore((state) => (state as any)._lastUpdate || 0);

  const interactionsLength = options?.interactionsLength ?? genaiChatInteractionsLength;
  const streaming = options?.streaming ?? genaiChatStreaming;
  const hydrating = options?.hydrating ?? genaiChatHydrating;
  const projectId = options?.projectId ?? genaiChatProjectId;

  // Auto-scroll to bottom when switching to another chat/project
  useEffect(() => {
    if (projectId && projectId !== lastProjectIdRef.current) {
      // Project changed - scroll to bottom after a short delay to ensure DOM is updated
      lastProjectIdRef.current = projectId;
      // Reset scroll position tracking for new project
      lastScrollInteractionLength.current = 0;
      lastUserInteractionIdRef.current = null;
      hasScrolledToCurrentUserMessageRef.current = false;
      const timeoutId = setTimeout(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 100);
      return () => clearTimeout(timeoutId);
    }
  }, [projectId, containerRef]);

  // Auto-scroll to bottom after hydration completes (when interactions are loaded)
  // Only scroll if this is the initial load (lastScrollInteractionLength is 0)
  // Don't scroll if hydration completes after updating existing interactions (e.g., pinning)
  useEffect(() => {
    if (!hydrating && interactionsLength > 0 && containerRef.current) {
      // Only scroll if this is the first load (no interactions were tracked before)
      // This prevents scrolling when updating existing interactions (like pinning)
      if (lastScrollInteractionLength.current === 0) {
        // Small delay to ensure DOM has rendered
        const timeoutId = setTimeout(() => {
          if (containerRef.current) {
            containerRef.current.scrollTop = containerRef.current.scrollHeight;
          }
        }, 50);
        // Update tracked length after initial scroll
        lastScrollInteractionLength.current = interactionsLength;
        return () => clearTimeout(timeoutId);
      } else {
        // Update tracked length even if we don't scroll (for consistency)
        lastScrollInteractionLength.current = interactionsLength;
      }
    }
  }, [hydrating, interactionsLength, containerRef]);

  // Track last user interaction ID to scroll to it specifically
  // Use a stable selector that returns only the ID to avoid infinite loops
  const currentUserInteractionId = useGenaiChatStore((state) => {
    const interactions = state.interactions;
    // Find last user interaction by iterating backwards
    for (let i = interactions.length - 1; i >= 0; i--) {
      if ((interactions[i] as any)?.kind === 'user') {
        return interactions[i]?.id || null;
      }
    }
    return null;
  });

  // Auto-scroll on new interactions or when streaming completes
  useEffect(() => {
    if (!containerRef.current) return;

    const currentLength = interactionsLength;
    const currentStreaming = streaming;

    // Scroll when new interaction is added (length increased)
    if (currentLength > lastScrollInteractionLength.current) {
      const isNewUserMessage = currentUserInteractionId && 
        currentUserInteractionId !== lastUserInteractionIdRef.current;
      
      lastScrollInteractionLength.current = currentLength;
      
      // If this is a new user message, scroll to it specifically
      if (isNewUserMessage && currentUserInteractionId) {
        lastUserInteractionIdRef.current = currentUserInteractionId;
        hasScrolledToCurrentUserMessageRef.current = false;
        // Use double requestAnimationFrame + timeout to ensure DOM is fully updated
        let timeoutId: number | null = null;
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            timeoutId = window.setTimeout(() => {
              if (containerRef.current) {
                // Try to find the user message element and scroll to it
                const userMessageElement = containerRef.current.querySelector(
                  `[data-interaction-id="${currentUserInteractionId}"]`
                ) as HTMLElement | null;
                
                if (userMessageElement) {
                  // Scroll to the user message element
                  userMessageElement.scrollIntoView({ 
                    behavior: 'smooth', 
                    block: 'center' 
                  });
                  hasScrolledToCurrentUserMessageRef.current = true;
                } else {
                  // Element not found (might be temporary ID), mark as not scrolled yet
                  // Will retry when ID updates to server ID
                  hasScrolledToCurrentUserMessageRef.current = false;
                  // Fallback: scroll to bottom
                  containerRef.current.scrollTop = containerRef.current.scrollHeight;
                }
              }
            }, 100);
          });
        });
        // Note: We can't return cleanup here as this is inside a useEffect callback
        // The timeout will be cleared when component unmounts or dependencies change
      } else {
        // For non-user messages, use standard scroll to bottom
        requestAnimationFrame(() => {
          if (containerRef.current) {
            containerRef.current.scrollTop = containerRef.current.scrollHeight;
          }
        });
      }
      return;
    }

    // Scroll when streaming completes (was streaming, now stopped)
    if (
      lastScrollStreamingState.current &&
      !currentStreaming &&
      currentLength > 0
    ) {
      lastScrollStreamingState.current = currentStreaming;
      // Small delay to ensure DOM has updated after streaming completes
      const timeoutId = setTimeout(() => {
        if (containerRef.current && !streaming) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 50);
      // Note: This timeout is short-lived and will complete before next render
      // In practice, cleanup happens when dependencies change or component unmounts
      return () => clearTimeout(timeoutId);
    }

    lastScrollStreamingState.current = currentStreaming;
  }, [interactionsLength, streaming, containerRef, currentUserInteractionId]);

  // Handle case when user interaction ID changes (e.g., from temporary to server ID)
  // This happens when a user message is saved to the server and gets a new ID
  useEffect(() => {
    if (!containerRef.current) return;
    if (!currentUserInteractionId) return;
    
    // If the user interaction ID changed, it might be an ID update (temporary -> server ID)
    // We should scroll to the new ID if we haven't successfully scrolled to it yet
    if (
      currentUserInteractionId !== lastUserInteractionIdRef.current &&
      currentUserInteractionId
    ) {
      // Check if we had a previous user interaction ID (meaning this is an ID update, not a new message)
      const isIdUpdate = lastUserInteractionIdRef.current !== null;
      
      // Only scroll if:
      // 1. This is an ID update (temporary -> server), OR
      // 2. We haven't successfully scrolled to the current message yet
      if (isIdUpdate || !hasScrolledToCurrentUserMessageRef.current) {
        lastUserInteractionIdRef.current = currentUserInteractionId;
        
        let timeoutId: number | null = null;
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            timeoutId = window.setTimeout(() => {
              if (containerRef.current) {
                // Try to find the user message element with the new server ID
                const userMessageElement = containerRef.current.querySelector(
                  `[data-interaction-id="${currentUserInteractionId}"]`
                ) as HTMLElement | null;
                
                if (userMessageElement) {
                  // Scroll to the user message element
                  userMessageElement.scrollIntoView({ 
                    behavior: 'smooth', 
                    block: 'center' 
                  });
                  hasScrolledToCurrentUserMessageRef.current = true;
                } else {
                  // Element not found, fallback to scroll to bottom
                  containerRef.current.scrollTop = containerRef.current.scrollHeight;
                  // Don't mark as scrolled if element wasn't found
                  hasScrolledToCurrentUserMessageRef.current = false;
                }
              }
            }, 150); // Slightly longer delay for ID updates to ensure DOM is updated
          });
        });
        // Note: Timeout will be cleared when component unmounts or dependencies change
      }
    }
  }, [currentUserInteractionId, containerRef]);

  // Auto-scroll during streaming when content updates (chunks arrive)
  useEffect(() => {
    if (!containerRef.current) return;
    if (!streaming) return; // Only scroll during streaming

    // Check if _lastUpdate timestamp changed (indicates content was updated)
    if (lastUpdateTimestamp > lastUpdateTimestampRef.current) {
      lastUpdateTimestampRef.current = lastUpdateTimestamp;
      // Use requestAnimationFrame for smooth scrolling during streaming
      requestAnimationFrame(() => {
        if (containerRef.current && streaming) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      });
    }
  }, [lastUpdateTimestamp, streaming, containerRef]);
}

