import { useEffect, RefObject } from 'react';

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

