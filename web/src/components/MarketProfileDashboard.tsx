import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ThemeContext } from './ThemeProvider';
import type { FragmentProfile } from './FragmentProfileModal';

export interface MarketProfileDashboardProps {
  profile: FragmentProfile | null;
}

type ProfileSection = FragmentProfile['sections'][string];
type ProfileDataset = NonNullable<ProfileSection['datasets']>[number];

function clamp01(v: number) {
  if (v < 0) return 0;
  if (v > 1) return 1;
  return v;
}

function formatProb(p: number) {
  const pp = clamp01(p);
  return `${Math.round(pp * 100)}%`;
}

function severityColor(severity: string | undefined) {
  switch ((severity || '').toLowerCase()) {
    case 'positive':
      return '#10b981';
    case 'warning':
      return '#f59e0b';
    case 'negative':
      return '#f87171';
    default:
      return 'var(--text-secondary-strong)';
  }
}

const SectionCard: React.FC<{
  title: string;
  right?: React.ReactNode;
  children: React.ReactNode;
  theme: string;
}> = ({ title, right, children, theme }) => {
  return (
    <div
      style={{
        borderRadius: 14,
        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
        background: theme === 'dark' ? 'rgba(0,0,0,0.14)' : 'rgba(0,0,0,0.02)',
        padding: 16,
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)' }}>{title}</div>
        {right}
      </div>
      {children}
    </div>
  );
};

const DatasetView: React.FC<{ dataset: ProfileDataset; theme: string }> = ({ dataset, theme }) => {
  const type = dataset.type;
  const series = dataset.series || [];

  // Lightweight chart primitives (no external deps):
  const renderTimeSeries = () => {
    const s0 = series[0];
    const pts = (s0?.points || []).filter((p: { y: number }) => Number.isFinite(p.y));
    if (pts.length < 2) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          Not enough data points to render a chart.
        </div>
      );
    }

    const w = 520;
    const h = 120;
    const pad = 10;
    const ys = pts.map((p) => p.y);
    const minY = Math.min(...ys);
    const maxY = Math.max(...ys);
    const range = maxY - minY || 1;

    const toX = (i: number) => pad + (i * (w - pad * 2)) / (pts.length - 1);
    const toY = (v: number) => pad + (h - pad * 2) * (1 - (v - minY) / range);

    const d = pts
      .map((p, i: number) => `${i === 0 ? 'M' : 'L'} ${toX(i).toFixed(2)} ${toY(p.y).toFixed(2)}`)
      .join(' ');

    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <svg width="100%" viewBox={`0 0 ${w} ${h}`} style={{ borderRadius: 10 }}>
          <path
            d={d}
            fill="none"
            stroke={theme === 'dark' ? 'rgba(59,130,246,0.9)' : 'rgba(59,130,246,0.95)'}
            strokeWidth="2.25"
          />
          {pts.map((p, i: number) => (
            <circle
              key={i}
              cx={toX(i)}
              cy={toY(p.y)}
              r={3}
              fill={theme === 'dark' ? 'rgba(59,130,246,1)' : 'rgba(59,130,246,1)'}
            />
          ))}
        </svg>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-secondary-strong)' }}>
          <span>{pts[0]?.x}</span>
          <span>
            {dataset.currency ? `${dataset.currency} ` : ''}
            {dataset.unit ? `(${dataset.unit})` : ''}
          </span>
          <span>{pts[pts.length - 1]?.x}</span>
        </div>
      </div>
    );
  };

  const renderBars = () => {
    const s0 = series[0];
    const pts = (s0?.points || []).filter((p: { y: number }) => Number.isFinite(p.y));
    if (pts.length === 0) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          No bar data available.
        </div>
      );
    }
    const top = [...pts].sort((a, b) => b.y - a.y).slice(0, 8);
    const maxV = Math.max(...top.map((p) => p.y)) || 1;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {top.map((p, idx) => (
          <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{ width: 220, fontSize: 12, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {p.x}
            </div>
            <div style={{ flex: 1, height: 10, borderRadius: 999, background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)', overflow: 'hidden' }}>
              <div
                style={{
                  width: `${Math.max(2, (p.y / maxV) * 100)}%`,
                  height: '100%',
                  background: 'rgba(59,130,246,0.85)',
                }}
              />
            </div>
            <div style={{ width: 90, textAlign: 'right', fontSize: 12, color: 'var(--text-secondary-strong)', fontFamily: 'monospace' }}>
              {p.y}
            </div>
          </div>
        ))}
      </div>
    );
  };

  const renderTable = () => {
    const table = dataset.table;
    const cols = table?.columns || [];
    const rows = table?.rows || [];
    if (!table || cols.length === 0 || rows.length === 0) {
      return (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
          No table data available.
        </div>
      );
    }
    return (
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
          <thead>
            <tr>
              {cols.map((c: { id: string; title: string }) => (
                <th
                  key={c.id}
                  style={{
                    textAlign: 'left',
                    padding: '10px 10px',
                    borderBottom: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                    color: 'var(--text-secondary-strong)',
                    fontWeight: 800,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {c.title || c.id}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, 20).map((r: Record<string, any>, i: number) => (
              <tr key={i}>
                {cols.map((c: { id: string }) => (
                  <td
                    key={c.id}
                    style={{
                      padding: '10px 10px',
                      borderBottom: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                      color: 'var(--text-heading)',
                      verticalAlign: 'top',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {String((r as any)[c.id] ?? '')}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  };

  return (
    <div
      style={{
        padding: '12px 14px',
        borderRadius: 12,
        border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
        background: theme === 'dark' ? 'rgba(0,0,0,0.10)' : 'rgba(0,0,0,0.015)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ fontSize: 13, fontWeight: 800, color: 'var(--text-heading)' }}>{dataset.title}</div>
        {dataset.confidence !== undefined && (
          <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>
            {Math.round(clamp01(dataset.confidence) * 100)}%
          </div>
        )}
      </div>
      {dataset.description && (
        <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
          {dataset.description}
        </div>
      )}
      {type === 'timeseries' && renderTimeSeries()}
      {(type === 'bar' || type === 'pie') && renderBars()}
      {type === 'table' && renderTable()}
      {(dataset.sources || []).length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>Sources:</div>
          {(dataset.sources || []).slice(0, 6).map((s: string, i: number) => (
            <a
              key={i}
              href={s}
              target="_blank"
              rel="noopener noreferrer"
              style={{ fontSize: 11, color: 'var(--accent-primary, #3B82F6)', textDecoration: 'none', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            >
              {s}
            </a>
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * Dashboard component for displaying Market fragment profiles.
 * Shows structured sections: overview, size_metrics, growth_outlook, competition, trends, barriers, regional.
 */
export const MarketProfileDashboard: React.FC<MarketProfileDashboardProps> = ({ profile }) => {
  const { theme } = React.useContext(ThemeContext);

  if (!profile) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        No profile data available
      </div>
    );
  }

  // Get sections in order
  const sectionKeys = Object.keys(profile.sections || {}).sort((a, b) => {
    const orderA = profile.sections[a]?.order || 100;
    const orderB = profile.sections[b]?.order || 100;
    return orderA - orderB;
  });

  if (sectionKeys.length === 0) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        Profile has no sections
      </div>
    );
  }

  return (
    <div
      style={{
        padding: '24px',
        display: 'flex',
        flexDirection: 'column',
        gap: 24,
      }}
    >
      {/* Profile metadata */}
      <div
        style={{
          padding: '12px 16px',
          borderRadius: 12,
          border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
          background: theme === 'dark' ? 'rgba(0,0,0,0.2)' : 'rgba(0,0,0,0.02)',
          fontSize: 11,
          color: 'var(--text-secondary-strong)',
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          flexWrap: 'wrap',
        }}
      >
        <div>
          <span style={{ fontWeight: 700 }}>Captured:</span>{' '}
          {new Date(profile.capturedAt).toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
          })}
        </div>
        {profile.generation?.groundingUsed && (
          <div>
            <span style={{ fontWeight: 700 }}>Grounding:</span> Enabled
          </div>
        )}
        {profile.generation?.model && (
          <div>
            <span style={{ fontWeight: 700 }}>Model:</span> {profile.generation.model}
          </div>
        )}
        {profile.generation?.graphContext && (
          <div>
            <span style={{ fontWeight: 700 }}>Context:</span>{' '}
            {profile.generation.graphContext.neighborCount} neighbors,{' '}
            {profile.generation.graphContext.edgeCount} edges
          </div>
        )}
      </div>

      {/* Dashboard blocks (v2+) */}
      {profile.dashboard && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {profile.dashboard.thesis && (
            <SectionCard
              theme={theme}
              title="Thesis"
              right={
                profile.dashboard.confidence !== undefined ? (
                  <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>
                    {Math.round(clamp01(profile.dashboard.confidence) * 100)}%
                  </div>
                ) : null
              }
            >
              <div style={{ fontSize: 13, color: 'var(--text-heading)', lineHeight: 1.6 }}>
                {profile.dashboard.thesis}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.kpis || []).length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
              {(profile.dashboard.kpis || []).slice(0, 12).map((kpi) => (
                <div
                  key={kpi.id}
                  style={{
                    flex: '1 1 180px',
                    minWidth: 180,
                    padding: '12px 14px',
                    borderRadius: 14,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.10)' : 'rgba(15,23,42,0.10)'}`,
                    background: theme === 'dark' ? 'rgba(0,0,0,0.12)' : 'rgba(0,0,0,0.015)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 6,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>{kpi.label}</div>
                    {kpi.confidence !== undefined && (
                      <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>
                        {Math.round(clamp01(kpi.confidence) * 100)}%
                      </div>
                    )}
                  </div>
                  <div style={{ fontSize: 16, fontWeight: 900, color: 'var(--text-heading)' }}>{kpi.value}</div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{kpi.subtext || ''}</div>
                    {kpi.trendValue ? (
                      <div style={{ fontSize: 11, fontWeight: 800, color: severityColor(kpi.severity) }}>{kpi.trendValue}</div>
                    ) : (
                      <div />
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {(profile.dashboard.scenarios || []).length > 0 && (
            <SectionCard theme={theme} title="Scenarios">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {(profile.dashboard.scenarios || []).slice(0, 6).map((sc) => (
                  <div
                    key={sc.id}
                    style={{
                      padding: 12,
                      borderRadius: 12,
                      border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                      background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.01)',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 10,
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                      <div style={{ fontSize: 13, fontWeight: 900, color: 'var(--text-heading)' }}>{sc.name}</div>
                      <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-secondary-strong)' }}>
                        {formatProb(sc.probability)}
                      </div>
                    </div>
                    {sc.metrics && Object.keys(sc.metrics).length > 0 && (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
                        {Object.entries(sc.metrics).slice(0, 6).map(([k, v]) => (
                          <div key={k} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                            <span style={{ fontWeight: 800 }}>{k}:</span> <span style={{ color: 'var(--text-heading)' }}>{v}</span>
                          </div>
                        ))}
                      </div>
                    )}
                    {(sc.assumptions || []).length > 0 && (
                      <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                        <div style={{ fontWeight: 800, marginBottom: 6 }}>Assumptions</div>
                        <ul style={{ margin: 0, paddingLeft: 18 }}>
                          {(sc.assumptions || []).slice(0, 5).map((a, i) => (
                            <li key={i}>{a}</li>
                          ))}
                        </ul>
                      </div>
                    )}
                    {(sc.triggers || []).length > 0 && (
                      <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                        <div style={{ fontWeight: 800, marginBottom: 6 }}>Triggers</div>
                        <ul style={{ margin: 0, paddingLeft: 18 }}>
                          {(sc.triggers || []).slice(0, 5).map((t, i) => (
                            <li key={i}>{t}</li>
                          ))}
                        </ul>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.watchlist || []).length > 0 && (
            <SectionCard theme={theme} title="Watchlist (leading indicators)">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {(profile.dashboard.watchlist || []).slice(0, 12).map((w) => (
                  <div
                    key={w.id}
                    style={{
                      padding: '10px 12px',
                      borderRadius: 12,
                      border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                      background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)',
                      display: 'grid',
                      gridTemplateColumns: '1.6fr 1fr 1fr 1.6fr',
                      gap: 10,
                      alignItems: 'baseline',
                    }}
                  >
                    <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>{w.indicator}</div>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                      <span style={{ fontWeight: 800 }}>Now:</span> <span style={{ color: 'var(--text-heading)' }}>{w.current || '—'}</span>
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                      <span style={{ fontWeight: 800 }}>Threshold:</span>{' '}
                      <span style={{ color: 'var(--text-heading)' }}>{w.threshold || '—'}</span>
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                      <span style={{ fontWeight: 800 }}>Signal:</span> <span style={{ color: 'var(--text-heading)' }}>{w.signal || '—'}</span>
                    </div>
                    {(w.sources || []).length > 0 && (
                      <div style={{ gridColumn: '1 / -1', display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                        {(w.sources || []).slice(0, 4).map((s, i) => (
                          <a
                            key={i}
                            href={s}
                            target="_blank"
                            rel="noopener noreferrer"
                            style={{ fontSize: 11, color: 'var(--accent-primary, #3B82F6)', textDecoration: 'none', maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                          >
                            {s}
                          </a>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.charts ?? []).length > 0 && (
            <SectionCard theme={theme} title="Dashboard charts">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {((profile.dashboard.charts ?? []) as ProfileDataset[]).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds} theme={theme} />
                ))}
              </div>
            </SectionCard>
          )}

          {(profile.dashboard.tables ?? []).length > 0 && (
            <SectionCard theme={theme} title="Dashboard tables">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {((profile.dashboard.tables ?? []) as ProfileDataset[]).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds} theme={theme} />
                ))}
              </div>
            </SectionCard>
          )}

          {profile.dashboard.notes && (
            <SectionCard theme={theme} title="Notes">
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.6 }}>
                {profile.dashboard.notes}
              </div>
            </SectionCard>
          )}
        </div>
      )}

      {/* Sections */}
      {sectionKeys.map((sectionKey) => {
        const section = profile.sections[sectionKey];
        if (!section) return null;

        const hasContent = Boolean(section.content?.trim());
        const hasStructured = section.structured && Object.keys(section.structured).length > 0;
        const hasSources = section.sources && section.sources.length > 0;
        const hasDatasets = (section.datasets || []).length > 0;

        if (!hasContent && !hasStructured && !hasDatasets) return null;

        return (
          <div
            key={sectionKey}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
            }}
          >
            {/* Section header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
              }}
            >
              <h3
                style={{
                  fontSize: 16,
                  fontWeight: 800,
                  color: 'var(--text-heading)',
                  margin: 0,
                }}
              >
                {section.title || sectionKey}
              </h3>
              {section.confidence !== undefined && (
                <div
                  style={{
                    fontSize: 11,
                    fontWeight: 700,
                    color:
                      section.confidence >= 0.8
                        ? '#10b981'
                        : section.confidence >= 0.6
                        ? '#f59e0b'
                        : '#f87171',
                    padding: '4px 10px',
                    borderRadius: 999,
                    background:
                      theme === 'dark'
                        ? 'rgba(255,255,255,0.06)'
                        : 'rgba(0,0,0,0.04)',
                  }}
                >
                  {Math.round(section.confidence * 100)}%
                </div>
              )}
            </div>

            {/* Structured data */}
            {hasStructured && (
              <div
                style={{
                  padding: '12px 16px',
                  borderRadius: 10,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.02)',
                  fontSize: 13,
                  fontFamily: 'monospace',
                }}
              >
                <pre
                  style={{
                    margin: 0,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    color: 'var(--text-heading)',
                  }}
                >
                  {JSON.stringify(section.structured, null, 2)}
                </pre>
              </div>
            )}

            {/* Datasets */}
            {hasDatasets && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                {(section.datasets || []).map((ds) => (
                  <DatasetView key={ds.id} dataset={ds as ProfileDataset} theme={theme} />
                ))}
              </div>
            )}

            {/* Content */}
            {hasContent && (
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--text-heading)',
                  lineHeight: 1.6,
                }}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {section.content}
                </ReactMarkdown>
              </div>
            )}

            {/* Sources */}
            {hasSources && (
              <div
                style={{
                  marginTop: 4,
                  paddingTop: 12,
                  borderTop: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                }}
              >
                <div
                  style={{
                    fontSize: 11,
                    fontWeight: 700,
                    color: 'var(--text-secondary-strong)',
                    marginBottom: 8,
                  }}
                >
                  Sources:
                </div>
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 6,
                  }}
                >
                  {(section.sources ?? []).map((source, idx) => (
                    <a
                      key={idx}
                      href={source}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{
                        fontSize: 11,
                        color: 'var(--accent-primary, #3B82F6)',
                        textDecoration: 'none',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        maxWidth: '100%',
                      }}
                      onMouseEnter={(e) => {
                        e.currentTarget.style.textDecoration = 'underline';
                      }}
                      onMouseLeave={(e) => {
                        e.currentTarget.style.textDecoration = 'none';
                      }}
                    >
                      {source}
                    </a>
                  ))}
                </div>
              </div>
            )}
          </div>
        );
      })}

      {/* Generation notes if available */}
      {profile.generation && (profile.generation as any).generationNotes && (
        <div
          style={{
            padding: '12px 16px',
            borderRadius: 10,
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
            background: theme === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.02)',
            fontSize: 12,
            color: 'var(--text-secondary-strong)',
            fontStyle: 'italic',
          }}
        >
          {(profile.generation as any).generationNotes}
        </div>
      )}
    </div>
  );
};
