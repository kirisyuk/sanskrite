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
      setTimeout(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 100);
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
        setTimeout(() => {
          if (containerRef.current) {
            containerRef.current.scrollTop = containerRef.current.scrollHeight;
          }
        }, 50);
        // Update tracked length after initial scroll
        lastScrollInteractionLength.current = interactionsLength;
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
        // Use double requestAnimationFrame + timeout to ensure DOM is fully updated
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            setTimeout(() => {
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
                } else {
                  // Fallback: scroll to bottom
                  containerRef.current.scrollTop = containerRef.current.scrollHeight;
                }
              }
            }, 100);
          });
        });
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
      setTimeout(() => {
        if (containerRef.current && !streaming) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 50);
      return;
    }

    lastScrollStreamingState.current = currentStreaming;
  }, [interactionsLength, streaming, containerRef, currentUserInteractionId]);

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

