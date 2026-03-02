import React, { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';

interface CollapsibleTextProps {
  children: React.ReactNode;
  maxLines?: number;
  className?: string;
}

export const CollapsibleText: React.FC<CollapsibleTextProps> = ({
  children,
  maxLines = 7,
  className = '',
}) => {
  const { t } = useTranslation();
  const [isExpanded, setIsExpanded] = useState(false);
  const [needsCollapse, setNeedsCollapse] = useState(false);
  const [maxHeight, setMaxHeight] = useState<number>(0);
  const contentRef = useRef<HTMLDivElement>(null);
  const isCheckingRef = useRef(false);

  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let resizeObserver: ResizeObserver | null = null;
    let debounceTimeout: ReturnType<typeof setTimeout> | null = null;
    
    const checkHeight = () => {
      if (isCheckingRef.current || !contentRef.current) return;
      
      isCheckingRef.current = true;
      
      // Temporarily disconnect ResizeObserver to prevent infinite loops
      if (resizeObserver) {
        resizeObserver.disconnect();
      }
      
      try {
        // Get computed line height
        const computedStyle = window.getComputedStyle(contentRef.current);
        const lineHeight = parseFloat(computedStyle.lineHeight);
        
        // Use line height if available, otherwise estimate (text-13 typically has ~20px line height)
        const effectiveLineHeight = lineHeight && !isNaN(lineHeight) && lineHeight > 0 ? lineHeight : 20;
        const calculatedMaxHeight = effectiveLineHeight * maxLines;
        
        // Temporarily remove max-height to measure full height
        const originalMaxHeight = contentRef.current.style.maxHeight;
        const originalOverflow = contentRef.current.style.overflow;
        contentRef.current.style.maxHeight = 'none';
        contentRef.current.style.overflow = 'visible';
        const actualHeight = contentRef.current.scrollHeight;
        contentRef.current.style.maxHeight = originalMaxHeight;
        contentRef.current.style.overflow = originalOverflow;
        
        // Only update state if values actually changed to prevent infinite loops
        setMaxHeight(prev => prev !== calculatedMaxHeight ? calculatedMaxHeight : prev);
        setNeedsCollapse(prev => {
          const newValue = actualHeight > calculatedMaxHeight;
          return prev !== newValue ? newValue : prev;
        });
      } finally {
        // Reconnect ResizeObserver after a delay to allow DOM to settle
        if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = setTimeout(() => {
          reconnectTimeoutRef.current = null;
          isCheckingRef.current = false;
          if (contentRef.current && resizeObserver) {
            resizeObserver.observe(contentRef.current);
          }
        }, 50);
      }
    };

    // Debounced version for ResizeObserver
    const debouncedCheckHeight = () => {
      if (debounceTimeout) {
        clearTimeout(debounceTimeout);
      }
      debounceTimeout = setTimeout(() => {
        if (!isCheckingRef.current) {
          checkHeight();
        }
      }, 100);
    };

    // Check immediately
    checkHeight();

    // Also check after delays to account for async rendering (e.g., markdown, images)
    const timeoutId1 = setTimeout(checkHeight, 100);
    const timeoutId2 = setTimeout(checkHeight, 500);
    const timeoutId3 = setTimeout(checkHeight, 1000);

    // Use ResizeObserver to detect when content size changes
    if (contentRef.current) {
      resizeObserver = new ResizeObserver(debouncedCheckHeight);
      resizeObserver.observe(contentRef.current);
    }

    return () => {
      clearTimeout(timeoutId1);
      clearTimeout(timeoutId2);
      clearTimeout(timeoutId3);
      if (debounceTimeout) {
        clearTimeout(debounceTimeout);
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
      if (resizeObserver) {
        resizeObserver.disconnect();
      }
    };
  }, [children, maxLines]);

  return (
    <div className={className}>
      <div
        ref={contentRef}
        style={{
          maxHeight: !isExpanded && needsCollapse && maxHeight > 0 ? `${maxHeight}px` : 'none',
          overflow: 'hidden',
          transition: 'max-height 0.2s ease-out',
        }}
      >
        {children}
      </div>
      {needsCollapse && (
        <button
          type="button"
          onClick={() => setIsExpanded(!isExpanded)}
          className="mt-1 text-[11px] underline"
          style={{ color: 'var(--text-secondary-strong)' }}
        >
          {isExpanded ? t('chatMessage.showLess', { defaultValue: 'Less...' }) : t('chatMessage.showMore', { defaultValue: 'More...' })}
        </button>
      )}
    </div>
  );
};

