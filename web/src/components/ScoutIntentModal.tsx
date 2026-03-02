import React from 'react';

export interface ScoutIntentResolution {
  focus?: string;
  spherePaths?: string[];
  keywords?: string[];
  anchors?: Array<{
    canonicalPath: string;
    type?: string;
    name?: string;
    confidence?: number;
    reason?: string;
  }>;
  domains?: Array<{
    canonicalPath: string;
    contractType?: string;
    title?: string;
    description?: string;
    firstOrderEntities?: string[];
    confidence?: number;
    reason?: string;
  }>;
  decompositionHints?: string[];
}

export interface ScoutIntentModalProps {
  visible: boolean;
  query: string;
  intent: ScoutIntentResolution | null;
  intentLoading: boolean;
  intentError: string | null;
  runLoading: boolean;
  progressLog: string[];
  onClose: () => void;
  onRunScout: () => void;
  theme?: 'light' | 'dark';
}

function humanizePath(cp: string): string {
  const parts = String(cp || '').trim().split('/').filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : cp || '?';
}

export function ScoutIntentModal({
  visible,
  query,
  intent,
  intentLoading,
  intentError,
  runLoading,
  progressLog,
  onClose,
  onRunScout,
  theme = 'dark',
}: ScoutIntentModalProps) {
  if (!visible) return null;

  const hasIntent = intent && (intent.anchors?.length || intent.domains?.length || intent.focus);
  const canRun = hasIntent && !runLoading;

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 10000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'rgba(0,0,0,0.5)',
        backdropFilter: 'blur(4px)',
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 560,
          maxWidth: '92vw',
          maxHeight: '85vh',
          borderRadius: 16,
          border: '1px solid var(--border-subtle)',
          background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
          boxShadow: theme === 'dark' ? '0 24px 64px rgba(0,0,0,0.6)' : '0 24px 64px rgba(0,0,0,0.2)',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        {/* Header */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid var(--border-subtle)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            flexShrink: 0,
          }}
        >
          <h3
            style={{
              margin: 0,
              fontSize: 16,
              fontWeight: 700,
              color: 'var(--text-heading)',
            }}
          >
            Scout: Intent Resolution
          </h3>
          <button
            type="button"
            onClick={onClose}
            style={{
              padding: 6,
              border: 'none',
              background: 'transparent',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
              borderRadius: 8,
            }}
            aria-label="Close"
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div style={{ padding: '16px 20px', overflowY: 'auto', flex: 1 }}>
          {/* Query */}
          <div style={{ marginBottom: 16 }}>
            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.4 }}>
              Query
            </div>
            <div style={{ fontSize: 14, color: 'var(--text-heading)', fontStyle: 'italic' }}>
              &quot;{query}&quot;
            </div>
          </div>

          {/* Intent loading */}
          {intentLoading && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 16 }}>
              <div style={{ display: 'flex', gap: 4 }}>
                {[0, 1, 2].map((i) => (
                  <div
                    key={i}
                    style={{
                      width: 8,
                      height: 8,
                      borderRadius: 999,
                      background: 'var(--accent-primary, #3B82F6)',
                      animation: 'scout-dot-bounce 1s infinite ease-in-out',
                      animationDelay: `${i * 0.12}s`,
                    }}
                  />
                ))}
              </div>
              <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>Resolving intent…</span>
            </div>
          )}

          {/* Intent error */}
          {intentError && (
            <div
              style={{
                padding: 12,
                borderRadius: 10,
                background: theme === 'dark' ? 'rgba(239,68,68,0.15)' : 'rgba(239,68,68,0.08)',
                border: '1px solid rgba(239,68,68,0.4)',
                color: 'var(--text-heading)',
                fontSize: 13,
                marginBottom: 16,
              }}
            >
              {intentError}
            </div>
          )}

          {/* Intent result */}
          {!intentLoading && intent && (
            <div style={{ marginBottom: 16 }}>
              <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 10, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                Resolved Intent
              </div>

              {intent.focus && (
                <div style={{ marginBottom: 10 }}>
                  <span style={{ fontSize: 11, color: 'var(--text-secondary)', marginRight: 8 }}>Focus:</span>
                  <span style={{ fontSize: 13, color: 'var(--text-heading)' }}>{intent.focus}</span>
                </div>
              )}

              {intent.anchors && intent.anchors.length > 0 && (
                <div style={{ marginBottom: 10 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginBottom: 6 }}>Anchors ({intent.anchors.length})</div>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                    {intent.anchors.map((a, i) => (
                      <span
                        key={i}
                        style={{
                          fontSize: 12,
                          padding: '4px 10px',
                          borderRadius: 8,
                          background: theme === 'dark' ? 'rgba(59,130,246,0.2)' : 'rgba(59,130,246,0.12)',
                          color: 'var(--text-heading)',
                          fontFamily: 'monospace',
                        }}
                        title={a.canonicalPath}
                      >
                        {a.name || humanizePath(a.canonicalPath)}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {intent.domains && intent.domains.length > 0 && (
                <div style={{ marginBottom: 10 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginBottom: 6 }}>Domains ({intent.domains.length})</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {intent.domains.map((d, i) => (
                      <div
                        key={i}
                        style={{
                          fontSize: 12,
                          padding: 8,
                          borderRadius: 8,
                          background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)',
                          border: '1px solid var(--border-subtle)',
                        }}
                      >
                        <div style={{ fontWeight: 600, color: 'var(--text-heading)', marginBottom: 2 }}>
                          {d.title || d.contractType || humanizePath(d.canonicalPath)}
                        </div>
                        {d.description && (
                          <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 4 }}>{d.description}</div>
                        )}
                        {d.firstOrderEntities && d.firstOrderEntities.length > 0 && (
                          <div style={{ fontSize: 10, color: 'var(--text-secondary)', marginTop: 4 }}>
                            +{d.firstOrderEntities.length} first-order entities
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {intent.keywords && intent.keywords.length > 0 && (
                <div style={{ marginBottom: 10 }}>
                  <span style={{ fontSize: 11, color: 'var(--text-secondary)', marginRight: 8 }}>Keywords:</span>
                  <span style={{ fontSize: 12, color: 'var(--text-heading)' }}>{intent.keywords.join(', ')}</span>
                </div>
              )}

              {!intent.focus && (!intent.anchors || intent.anchors.length === 0) && (!intent.domains || intent.domains.length === 0) && (
                <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>No anchors or domains resolved.</div>
              )}
            </div>
          )}

          {/* Progress log */}
          {progressLog.length > 0 && (
            <div style={{ marginTop: 16 }}>
              <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                Progress
              </div>
              <div
                style={{
                  maxHeight: 120,
                  overflowY: 'auto',
                  padding: 10,
                  borderRadius: 8,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.3)' : 'rgba(0,0,0,0.05)',
                  fontFamily: 'monospace',
                  fontSize: 12,
                  color: 'var(--text-secondary)',
                  lineHeight: 1.6,
                }}
              >
                {progressLog.map((line, i) => (
                  <div key={i}>{line}</div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div
          style={{
            padding: '12px 20px 16px',
            borderTop: '1px solid var(--border-subtle)',
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 10,
            flexShrink: 0,
          }}
        >
          <button
            type="button"
            onClick={onClose}
            style={{
              padding: '8px 16px',
              borderRadius: 8,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-secondary)',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            {progressLog.length > 0 ? 'Close' : 'Cancel'}
          </button>
          <button
            type="button"
            onClick={onRunScout}
            disabled={!canRun}
            style={{
              padding: '8px 20px',
              borderRadius: 8,
              border: 'none',
              background: canRun ? 'var(--accent-primary, #3B82F6)' : (theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.08)'),
              color: canRun ? '#fff' : 'var(--text-secondary)',
              fontSize: 13,
              fontWeight: 600,
              cursor: canRun ? 'pointer' : 'not-allowed',
            }}
          >
            {runLoading ? 'Running…' : 'Run Scout'}
          </button>
        </div>
      </div>

      <style>{`
        @keyframes scout-dot-bounce {
          0%, 80%, 100% { transform: translateY(0); opacity: 0.5; }
          40% { transform: translateY(-4px); opacity: 1; }
        }
      `}</style>
    </div>
  );
}
