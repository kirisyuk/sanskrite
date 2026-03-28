import React from 'react';

export type LensBottomPaletteTheme = 'light' | 'dark';

export type LensBottomPaletteChrome = {
  triggerBorder: string;
  triggerBackground: string;
  triggerShadow: string;
  triggerOutline: string;
  overlayBackground: string;
  panelBackground: string;
  panelBorder: string;
  panelShadow: string;
  activeItemBackground: string;
  hoverItemBackground: string;
  activeItemBorder: string;
  subtleSurface: string;
};

export const buildLensBottomPaletteChrome = (
  theme: LensBottomPaletteTheme,
  open: boolean,
): LensBottomPaletteChrome => {
  const isDark = theme === 'dark';
  return {
    triggerBorder: open
      ? (isDark ? '1px solid rgba(96,165,250,0.55)' : '1px solid rgba(37,99,235,0.50)')
      : (isDark ? '1px solid rgba(255,255,255,0.16)' : '1px solid rgba(15,23,42,0.12)'),
    triggerBackground: open
      ? (isDark ? 'rgba(59,130,246,0.22)' : 'rgba(59,130,246,0.14)')
      : (isDark ? 'rgba(6,10,18,0.72)' : 'rgba(255,255,255,0.86)'),
    triggerShadow: isDark ? 'drop-shadow(0 16px 40px rgba(0,0,0,0.55))' : 'drop-shadow(0 16px 40px rgba(0,0,0,0.18))',
    triggerOutline: open
      ? (isDark ? 'inset 0 0 0 1px rgba(125,211,252,0.18)' : 'inset 0 0 0 1px rgba(59,130,246,0.10)')
      : (isDark ? 'inset 0 0 0 1px rgba(255,255,255,0.05)' : 'inset 0 0 0 1px rgba(255,255,255,0.55)'),
    overlayBackground: isDark ? 'rgba(0,0,0,0.5)' : 'rgba(255,255,255,0.4)',
    panelBackground: 'var(--bg-elevated)',
    panelBorder: '1px solid var(--border-subtle)',
    panelShadow: isDark ? '0 20px 60px rgba(0,0,0,0.6)' : '0 20px 60px rgba(0,0,0,0.2)',
    activeItemBackground: isDark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)',
    hoverItemBackground: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
    activeItemBorder: 'var(--accent-primary, #3B82F6)',
    subtleSurface: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
  };
};

export const LensBottomPaletteTrigger: React.FC<{
  open: boolean;
  disabled?: boolean;
  onClick: (event: React.MouseEvent<HTMLButtonElement>) => void;
  title: string;
  ariaLabel: string;
  chrome: LensBottomPaletteChrome;
  bottom?: number;
  zIndex?: number;
  children?: React.ReactNode;
}> = ({
  open,
  disabled = false,
  onClick,
  title,
  ariaLabel,
  chrome,
  bottom = 14,
  zIndex = 220,
  children,
}) => (
  <div
    style={{
      position: 'absolute',
      left: '50%',
      bottom,
      transform: 'translateX(-50%)',
      zIndex,
      pointerEvents: 'none',
    }}
  >
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      aria-label={ariaLabel}
      style={{
        pointerEvents: 'auto',
        width: 44,
        height: 44,
        borderRadius: 999,
        border: chrome.triggerBorder,
        background: chrome.triggerBackground,
        color: 'var(--text-heading)',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        filter: chrome.triggerShadow,
        boxShadow: chrome.triggerOutline,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        backdropFilter: 'blur(8px)',
        transition: 'background-color 0.18s ease, border-color 0.18s ease, box-shadow 0.18s ease',
      }}
      data-open={open ? 'true' : 'false'}
    >
      {children}
    </button>
  </div>
);

export const LensBottomPaletteShell: React.FC<{
  open: boolean;
  onBackdropClick: () => void;
  chrome: LensBottomPaletteChrome;
  width?: number;
  maxWidth?: string;
  paddingBottom?: number;
  zIndex?: number;
  topContent?: React.ReactNode;
  children: React.ReactNode;
}> = ({
  open,
  onBackdropClick,
  chrome,
  width = 520,
  maxWidth = '90vw',
  paddingBottom = 72,
  zIndex = 260,
  topContent,
  children,
}) => {
  if (!open) return null;
  return (
    <div
      style={{
        position: 'absolute',
        inset: 0,
        zIndex,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'flex-end',
        paddingBottom,
        background: chrome.overlayBackground,
        backdropFilter: 'blur(4px)',
        animation: 'lens-bottom-palette-backdrop-in 0.12s ease-out',
      }}
      onClick={onBackdropClick}
    >
      <style>{`
        @keyframes lens-bottom-palette-backdrop-in {
          from { opacity: 0; }
          to { opacity: 1; }
        }
        @keyframes lens-bottom-palette-rise {
          from { opacity: 0; transform: translateY(14px) scale(0.98); }
          to { opacity: 1; transform: scale(1) translateY(0); }
        }
      `}</style>
      {topContent ? (
        <div
          onClick={(event) => event.stopPropagation()}
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            gap: 8,
            marginBottom: 8,
          }}
        >
          {topContent}
        </div>
      ) : null}
      <div
        onClick={(event) => event.stopPropagation()}
        style={{
          width,
          maxWidth,
          display: 'flex',
          flexDirection: 'column',
          background: chrome.panelBackground,
          border: chrome.panelBorder,
          borderRadius: 16,
          boxShadow: chrome.panelShadow,
          overflow: 'hidden',
          color: 'var(--text-heading)',
          animation: 'lens-bottom-palette-rise 0.2s cubic-bezier(0.2, 0.9, 0.2, 1)',
          transformOrigin: '50% 100%',
        }}
      >
        {children}
      </div>
    </div>
  );
};
