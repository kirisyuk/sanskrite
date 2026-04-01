import React from 'react';
import type { ScoutingMode } from '../features/horizon/lib/scoutingMode';

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
    supplyChainExecutionPlan?: {
      graphBuildMode?: string;
      queries?: Array<{
        direction?: string;
      }>;
    };
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
  mode: ScoutingMode;
  showHotPathOption?: boolean;
  hotPathOnly?: boolean;
  onHotPathOnlyChange?: (next: boolean) => void;
  supplyChainDirection?: 'upstream' | 'downstream' | 'both';
  onSupplyChainDirectionChange?: (next: 'upstream' | 'downstream' | 'both') => void;
  onModeChange: (mode: ScoutingMode) => void;
  onClose: () => void;
  onRunScout: () => void;
  theme?: 'light' | 'dark';
}

function humanizePath(cp: string): string {
  const parts = String(cp || '').trim().split('/').filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : cp || '?';
}

function isSupplyChainContractType(value?: string): boolean {
  return /^(supply-chain|value-chain)$/i.test(String(value || '').trim());
}

function titleCaseWords(value: string): string {
  return String(value || '')
    .trim()
    .split(/\s+/g)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function humanizeContractType(value?: string): string {
  const contractType = String(value || '').trim().toLowerCase();
  if (!contractType) return 'Domain';
  if (contractType === 'supply-chain') return 'Supply chain';
  if (contractType === 'value-chain') return 'Value chain';
  return titleCaseWords(contractType.replace(/[-_]+/g, ' '));
}

function getSupplyChainGraphBuildModeLabel(value?: string): string {
  const mode = String(value || '').trim().toLowerCase();
  if (mode === 'hot_path_only') return 'Execution mode: Hot path only';
  if (mode === 'hot_path_plus_graph') return 'Execution mode: Hot path + graph';
  return '';
}

function getSupplyChainDirectionLabel(direction?: string): string {
  const value = String(direction || '').trim().toLowerCase();
  if (value === 'upstream') return 'Direction: Upstream';
  if (value === 'downstream') return 'Direction: Downstream';
  if (value === 'both') return 'Direction: Both';
  return '';
}

export function ScoutIntentModal({
  visible,
  query,
  intent,
  intentLoading,
  intentError,
  runLoading,
  progressLog,
  mode,
  showHotPathOption = false,
  hotPathOnly = false,
  onHotPathOnlyChange,
  supplyChainDirection = 'downstream',
  onSupplyChainDirectionChange,
  onModeChange,
  onClose,
  onRunScout,
  theme = 'dark',
}: ScoutIntentModalProps) {
  if (!visible) return null;

  const hasIntent = intent && (intent.anchors?.length || intent.domains?.length || intent.focus);
  const canRun = hasIntent && !runLoading;
  const resolvedDomainTypes = Array.from(
    new Set(
      (intent?.domains || [])
        .map((domain) => humanizeContractType(domain?.contractType))
        .filter(Boolean),
    ),
  );
  const runBehaviorLabel = (() => {
    if (mode === 'supply_chain') {
      if (hotPathOnly && supplyChainDirection !== 'both') {
        return `Supply-chain hot path ${supplyChainDirection}`;
      }
      if (supplyChainDirection === 'both') {
        return 'Supply-chain full graph expansion';
      }
      return `Supply-chain directed expansion (${supplyChainDirection})`;
    }
    if (mode === 'governance') return 'Governance expansion';
    return 'General graph expansion';
  })();

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
              <div style={{ marginBottom: 14 }}>
                <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                  Scouting Mode
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 8 }}>
                  {([
                    { value: 'general' as const, label: 'General', desc: 'Broad exploration' },
                    { value: 'supply_chain' as const, label: 'Supply chain', desc: 'Operational and product flows' },
                    { value: 'governance' as const, label: 'Governance', desc: 'Ownership, control, leadership' },
                  ]).map((opt) => {
                    const active = mode === opt.value;
                    return (
                      <button
                        key={opt.value}
                        type="button"
                        onClick={() => onModeChange(opt.value)}
                        disabled={runLoading}
                        aria-pressed={active}
                        style={{
                          textAlign: 'left',
                          padding: '10px 10px 9px',
                          borderRadius: 10,
                          border: '1px solid var(--border-subtle)',
                          background: active ? 'var(--white-10)' : 'transparent',
                          color: 'var(--text-heading)',
                          cursor: runLoading ? 'not-allowed' : 'pointer',
                          opacity: runLoading ? 0.5 : 1,
                        }}
                      >
                        <div style={{ fontSize: 12, fontWeight: 700, lineHeight: 1.2 }}>{opt.label}</div>
                        <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                          {opt.desc}
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>

              {showHotPathOption && mode === 'supply_chain' && (
                <div style={{ marginBottom: 14 }}>
                  <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                    Supply Chain Execution
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 8, marginBottom: 10 }}>
                    {([
                      { value: 'downstream' as const, label: 'Downstream', desc: 'Follow exposed flows, customers, and dependencies.' },
                      { value: 'upstream' as const, label: 'Upstream', desc: 'Trace suppliers, inputs, and source dependencies.' },
                      { value: 'both' as const, label: 'Both', desc: 'Keep the broader bidirectional chain mapping.' },
                    ]).map((opt) => {
                      const active = supplyChainDirection === opt.value;
                      return (
                        <button
                          key={opt.value}
                          type="button"
                          onClick={() => onSupplyChainDirectionChange?.(opt.value)}
                          disabled={runLoading}
                          aria-pressed={active}
                          style={{
                            textAlign: 'left',
                            padding: '10px 10px 9px',
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: active ? 'var(--white-10)' : 'transparent',
                            color: 'var(--text-heading)',
                            cursor: runLoading ? 'not-allowed' : 'pointer',
                            opacity: runLoading ? 0.5 : 1,
                          }}
                        >
                          <div style={{ fontSize: 12, fontWeight: 700, lineHeight: 1.2 }}>{opt.label}</div>
                          <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                            {opt.desc}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                  <label
                    style={{
                      display: 'grid',
                      gridTemplateColumns: '16px minmax(0, 1fr)',
                      gap: 10,
                      alignItems: 'flex-start',
                      cursor: runLoading ? 'not-allowed' : 'pointer',
                      opacity: runLoading ? 0.5 : 1,
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={hotPathOnly}
                      onChange={(e) => onHotPathOnlyChange?.(e.target.checked)}
                      disabled={runLoading}
                      style={{ marginTop: 2 }}
                    />
                    <span style={{ display: 'grid', gap: 2 }}>
                      <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>Only hot path</span>
                      <span style={{ fontSize: 11, lineHeight: 1.35, color: 'var(--text-secondary-strong)' }}>
                        Run the fast operational chain first instead of the full graph expansion. `Both` keeps the broader full pass.
                      </span>
                    </span>
                  </label>
                </div>
              )}

              <div style={{ marginBottom: 14 }}>
                <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 8, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                  Run Behavior
                </div>
                <div style={{ display: 'grid', gap: 8 }}>
                  <div style={{ fontSize: 13, color: 'var(--text-heading)' }}>{runBehaviorLabel}</div>
                  {resolvedDomainTypes.length > 0 && (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                      {resolvedDomainTypes.map((label) => (
                        <span
                          key={label}
                          style={{
                            fontSize: 10,
                            padding: '3px 8px',
                            borderRadius: 999,
                            border: '1px solid var(--border-subtle)',
                            background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.04)',
                            color: 'var(--text-secondary-strong)',
                            fontWeight: 700,
                            letterSpacing: 0.2,
                          }}
                        >
                          Resolved: {label}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              </div>

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
                    {intent.domains.map((d, i) => {
                      const contractTypeLabel = humanizeContractType(d.contractType);
                      const graphBuildModeLabel = isSupplyChainContractType(d.contractType) ? getSupplyChainGraphBuildModeLabel(d.supplyChainExecutionPlan?.graphBuildMode) : '';
                      const directionLabel = isSupplyChainContractType(d.contractType)
                        ? getSupplyChainDirectionLabel(d.supplyChainExecutionPlan?.queries?.[0]?.direction)
                        : '';
                      return (
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
                            {d.title || contractTypeLabel || humanizePath(d.canonicalPath)}
                          </div>
                          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 6 }}>
                            <span
                              style={{
                                fontSize: 10,
                                padding: '3px 8px',
                                borderRadius: 999,
                                border: '1px solid var(--border-subtle)',
                                background: theme === 'dark' ? 'rgba(59,130,246,0.14)' : 'rgba(59,130,246,0.10)',
                                color: 'var(--text-heading)',
                                fontWeight: 700,
                                letterSpacing: 0.2,
                              }}
                            >
                              Type: {contractTypeLabel}
                            </span>
                            {directionLabel && (
                              <span
                                style={{
                                  fontSize: 10,
                                  padding: '3px 8px',
                                  borderRadius: 999,
                                  border: '1px solid var(--border-subtle)',
                                  background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.04)',
                                  color: 'var(--text-secondary-strong)',
                                  fontWeight: 700,
                                  letterSpacing: 0.2,
                                }}
                              >
                                {directionLabel}
                              </span>
                            )}
                            {graphBuildModeLabel && (
                              <span
                                style={{
                                  fontSize: 10,
                                  padding: '3px 8px',
                                  borderRadius: 999,
                                  border: '1px solid var(--border-subtle)',
                                  background: theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.04)',
                                  color: 'var(--text-secondary-strong)',
                                  fontWeight: 700,
                                  letterSpacing: 0.2,
                                }}
                              >
                                {graphBuildModeLabel}
                              </span>
                            )}
                          </div>
                          {d.description && (
                            <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 4 }}>{d.description}</div>
                          )}
                          {d.firstOrderEntities && d.firstOrderEntities.length > 0 && (
                            <div style={{ fontSize: 10, color: 'var(--text-secondary)', marginTop: 4 }}>
                              +{d.firstOrderEntities.length} first-order entities
                            </div>
                          )}
                          {d.canonicalPath && (
                            <div
                              style={{
                                fontSize: 10,
                                color: 'var(--text-tertiary)',
                                marginTop: 6,
                                fontFamily: 'monospace',
                                wordBreak: 'break-all',
                              }}
                            >
                              {d.canonicalPath}
                            </div>
                          )}
                        </div>
                      );
                    })}
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
