import React, { useEffect, useState, useRef } from 'react';

interface ExpandEmptyToastProps {
  visible: boolean;
  onHide: () => void;
  message?: string;
  subMessage?: string;
  duration?: number; // ms before auto-hide
  actionLabel?: string;
  onAction?: () => void;
  actionDisabled?: boolean;
}

/**
 * Toast notification for empty expand results.
 * Strict corporate style matching HorizonRealtimeCallouts.
 * Appears at bottom-center with fade animations.
 */
export const ExpandEmptyToast: React.FC<ExpandEmptyToastProps> = ({
  visible,
  onHide,
  message = 'No related objects found',
  subMessage = 'Try a different expansion mode',
  duration = 2000,
  actionLabel,
  onAction,
  actionDisabled = false,
}) => {
  const [show, setShow] = useState(false);
  const [opacity, setOpacity] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hideRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleClose = () => {
    if (timerRef.current) clearTimeout(timerRef.current);
    if (hideRef.current) clearTimeout(hideRef.current);
    setOpacity(0);
    hideRef.current = setTimeout(() => {
      setShow(false);
      onHide();
    }, 300);
  };

  useEffect(() => {
    if (visible) {
      // Clear any existing timer
      if (timerRef.current) clearTimeout(timerRef.current);
      
      // Show and fade in
      setShow(true);
      requestAnimationFrame(() => setOpacity(1));
      
      // Auto-hide after duration
      timerRef.current = setTimeout(() => {
        setOpacity(0);
        // Wait for fade out, then hide and notify parent
        hideRef.current = setTimeout(() => {
          setShow(false);
          onHide();
        }, 300);
      }, duration);
    }
    
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (hideRef.current) clearTimeout(hideRef.current);
    };
  }, [visible, duration, onHide]);

  if (!show) return null;

  return (
    <div
      style={{
        position: 'fixed',
        bottom: 24,
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 9999,
        pointerEvents: 'none',
      }}
    >
      <div
        style={{
          position: 'relative',
          display: 'flex',
          alignItems: 'flex-start',
          gap: 10,
          width: 320,
          maxWidth: 'min(320px, calc(100vw - 32px))',
          borderRadius: 12,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
          boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
          padding: '10px 34px 10px 12px',
          opacity: opacity,
          transition: 'opacity 0.3s ease',
          pointerEvents: 'auto',
        }}
      >
        <button
          type="button"
          onClick={handleClose}
          aria-label="Close"
          style={{
            position: 'absolute',
            top: 6,
            right: 6,
            width: 20,
            height: 20,
            borderRadius: 6,
            border: '1px solid transparent',
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'pointer',
          }}
        >
          <svg
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
        {/* Icon */}
        <div
          style={{
            flexShrink: 0,
            width: 20,
            height: 20,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#F59E0B"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="8" x2="12" y2="12" />
            <line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
        </div>

        {/* Text content */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 700,
              color: 'var(--text-heading)',
            }}
          >
            {message}
          </div>
          {subMessage && (
            <div
              style={{
                marginTop: 4,
                fontSize: 11,
                color: 'var(--text-secondary-strong)',
              }}
            >
              {subMessage}
            </div>
          )}
        </div>
        {actionLabel && onAction && (
          <button
            type="button"
            onClick={onAction}
            disabled={actionDisabled}
            style={{
              alignSelf: 'center',
              padding: '6px 10px',
              borderRadius: 8,
              border: '1px solid var(--border-subtle)',
              background: actionDisabled ? 'transparent' : 'var(--accent-primary, #3B82F6)',
              color: actionDisabled ? 'var(--text-secondary-strong)' : '#fff',
              fontSize: 11,
              fontWeight: 800,
              cursor: actionDisabled ? 'not-allowed' : 'pointer',
              opacity: actionDisabled ? 0.6 : 1,
              transition: 'transform 120ms ease, box-shadow 120ms ease',
              boxShadow: 'none',
              pointerEvents: actionDisabled ? 'none' : 'auto',
            }}
          >
            {actionLabel}
          </button>
        )}
      </div>
    </div>
  );
};

export default ExpandEmptyToast;
