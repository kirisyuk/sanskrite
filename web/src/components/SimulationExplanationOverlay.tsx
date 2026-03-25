import React from 'react';
import type {
  HorizonSimulationExplanationPayload,
  HorizonSimulationOverlayItem,
} from '../lib/api';

type ExplanationStatus = 'idle' | 'loading' | 'ready' | 'error';

export interface SimulationExplanationOverlayState {
  status: ExplanationStatus;
  data?: HorizonSimulationExplanationPayload;
  error?: string;
}

interface SimulationExplanationOverlayProps {
  isDark: boolean;
  lensId: string;
  runId: string;
  overlay: HorizonSimulationOverlayItem;
  state: SimulationExplanationOverlayState;
  additionalSelectedCount?: number;
}

function BouncingBalls({ isDark }: { isDark: boolean }) {
  const dotStyle: React.CSSProperties = {
    width: 8,
    height: 8,
    borderRadius: 999,
    background: isDark ? '#fca5a5' : '#dc2626',
    display: 'inline-block',
    animation: 'simulation-bounce 0.9s infinite ease-in-out',
  };
  return (
    <>
      <style>{`
        @keyframes simulation-bounce {
          0%, 80%, 100% { transform: translateY(0); opacity: 0.45; }
          40% { transform: translateY(-7px); opacity: 1; }
        }
      `}</style>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span style={{ ...dotStyle, animationDelay: '0s' }} />
        <span style={{ ...dotStyle, animationDelay: '0.12s' }} />
        <span style={{ ...dotStyle, animationDelay: '0.24s' }} />
      </div>
    </>
  );
}

export function SimulationExplanationOverlay({
  isDark,
  lensId,
  runId,
  overlay,
  state,
  additionalSelectedCount = 0,
}: SimulationExplanationOverlayProps) {
  const cardStyle: React.CSSProperties = {
    position: 'absolute',
    top: 74,
    left: 18,
    zIndex: 35,
    width: 360,
    maxHeight: 'calc(100vh - 140px)',
    overflowY: 'auto',
    borderRadius: 16,
    border: '1px solid rgba(239,68,68,0.22)',
    background: isDark ? 'rgba(8,12,18,0.92)' : 'rgba(255,255,255,0.95)',
    boxShadow: isDark ? '0 20px 60px rgba(0,0,0,0.55)' : '0 20px 60px rgba(0,0,0,0.18)',
    backdropFilter: 'blur(14px)',
    padding: 14,
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    pointerEvents: 'auto',
  };

  const sectionTitleStyle: React.CSSProperties = {
    fontSize: 11,
    fontWeight: 900,
    letterSpacing: '0.08em',
    textTransform: 'uppercase',
    color: 'var(--text-secondary-strong)',
  };

  const listCard = (items: string[]) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      {items.slice(0, 5).map((item, index) => (
        <div
          key={`${item}-${index}`}
          style={{
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.03)',
            padding: '8px 10px',
            fontSize: 12,
            lineHeight: 1.45,
            color: 'var(--text-secondary)',
          }}
        >
          {item}
        </div>
      ))}
    </div>
  );

  return (
    <div style={cardStyle}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
          Lens {lensId} • Run {runId}
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'flex-start' }}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)' }}>
              {state.data?.title || overlay.title || overlay.id || 'Simulation explanation'}
            </div>
            <div style={{ marginTop: 3, fontSize: 11, color: '#ef4444', fontWeight: 800 }}>
              {overlay.riskCategory || overlay.kind || 'stress factor'}
            </div>
          </div>
          {additionalSelectedCount > 0 ? (
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
              +{additionalSelectedCount} selected
            </div>
          ) : null}
        </div>
      </div>

      {state.status === 'loading' ? (
        <div
          style={{
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.03)',
            padding: '14px 12px',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
            Preparing AI explanation from simulation traces and stress metrics.
          </div>
          <BouncingBalls isDark={isDark} />
        </div>
      ) : null}

      {state.status === 'error' ? (
        <div
          style={{
            borderRadius: 12,
            border: '1px solid rgba(239,68,68,0.28)',
            background: isDark ? 'rgba(239,68,68,0.12)' : 'rgba(239,68,68,0.08)',
            color: isDark ? '#fecaca' : '#991b1b',
            padding: '12px 14px',
            fontSize: 12,
            lineHeight: 1.4,
          }}
        >
          {state.error || 'Failed to generate simulation explanation.'}
        </div>
      ) : null}

      {state.status === 'ready' && state.data ? (
        <>
          <div
            style={{
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.03)',
              padding: '12px 14px',
              fontSize: 12,
              lineHeight: 1.55,
              color: 'var(--text-secondary)',
            }}
          >
            {state.data.summary}
          </div>

          {state.data.stressNarrative?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Why It Matters</div>
              {listCard(state.data.stressNarrative)}
            </div>
          ) : null}

          {state.data.affectedObjects?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Affected Objects</div>
              {listCard(state.data.affectedObjects)}
            </div>
          ) : null}

          {state.data.affectedLinks?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Affected Links</div>
              {listCard(state.data.affectedLinks)}
            </div>
          ) : null}

          {state.data.cascadeRisks?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Cascade Risks</div>
              {listCard(state.data.cascadeRisks)}
            </div>
          ) : null}

          {state.data.roleImplications?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>C-Level Implications</div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {state.data.roleImplications.slice(0, 4).map((item, index) => (
                  <div
                    key={`${item.role}-${index}`}
                    style={{
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: isDark ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.03)',
                      padding: '8px 10px',
                    }}
                  >
                    <div style={{ fontSize: 11, fontWeight: 900, color: 'var(--text-heading)' }}>{item.role}</div>
                    <div style={{ marginTop: 3, fontSize: 12, lineHeight: 1.45, color: 'var(--text-secondary)' }}>{item.implication}</div>
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          {state.data.recommendedActions?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Actions</div>
              {listCard(state.data.recommendedActions)}
            </div>
          ) : null}

          {state.data.keyMetrics?.length ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div style={sectionTitleStyle}>Key Metrics</div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8 }}>
                {state.data.keyMetrics.slice(0, 6).map((metric, index) => (
                  <React.Fragment key={`${metric.label}-${index}`}>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{metric.label}</div>
                    <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>{metric.value}</div>
                  </React.Fragment>
                ))}
              </div>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
