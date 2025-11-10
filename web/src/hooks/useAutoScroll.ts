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
  
  const genaiChat = useGenaiChatStore();
  const genaiChatInteractionsLength = useGenaiChatStore(
    (state) => state.interactions.length,
  );
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  const genaiChatHydrating = useGenaiChatStore((state) => state.hydrating);
  const genaiChatProjectId = useGenaiChatStore((state) => state.projectId);

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
      setTimeout(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 100);
    }
  }, [projectId, containerRef]);

  // Auto-scroll to bottom after hydration completes (when interactions are loaded)
  useEffect(() => {
    if (!hydrating && interactionsLength > 0 && containerRef.current) {
      // Small delay to ensure DOM has rendered
      setTimeout(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      }, 50);
    }
  }, [hydrating, interactionsLength, containerRef]);

  // Auto-scroll on new interactions or when streaming completes
  useEffect(() => {
    if (!containerRef.current) return;

    const currentLength = interactionsLength;
    const currentStreaming = streaming;

    // Scroll when new interaction is added (length increased)
    if (currentLength > lastScrollInteractionLength.current) {
      lastScrollInteractionLength.current = currentLength;
      requestAnimationFrame(() => {
        if (containerRef.current) {
          containerRef.current.scrollTop = containerRef.current.scrollHeight;
        }
      });
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
  }, [interactionsLength, streaming, containerRef]);
}

