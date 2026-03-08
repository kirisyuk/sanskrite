/**
 * Lens Q&A — Analysis view (plan §10).
 * Command Palette input, answer display, impact chain, evidence.
 */

import React from 'react';
import { api } from '../lib/api';

export interface QAPanelProps {
  lensId: string;
  projectId?: string;
  onHighlightNodes?: (nodeIds: string[]) => void;
  onHighlightEdges?: (edgeIds: string[]) => void;
  onHighlightTracePaths?: (paths: unknown[]) => void;
  externalOpenTrigger?: number;
}

type QAStatus =
  | 'idle'
  | 'decomposing'
  | 'simulating'
  | 'synthesizing'
  | 'sufficiency_check'
  | 'scouting'
  | 'filling_gaps'
  | 'answered'
  | 'failed';

type QAAnswer = {
  executiveSummary?: string;
  riskLevel?: string;
  riskScenario?: string;
  recommendations?: string[];
  firstOrderEffects?: Array<{
    summary?: string;
    horizon?: string;
    severity?: string;
    affectedEntities?: string[];
    mechanism?: string;
  }>;
  secondOrderEffects?: Array<{
    summary?: string;
    horizon?: string;
    severity?: string;
    affectedEntities?: string[];
    mechanism?: string;
  }>;
  watchpoints?: Array<{
    signal?: string;
    whyItMatters?: string;
    expectedWithin?: string;
  }>;
  assumptions?: string[];
  timeHorizon?: string;
  answerMode?: string;
  text: string;
  confidence: number;
  evidence?: Array<{ type?: string; canonicalPath?: string; summary?: string; confidence?: number; verified?: boolean }>;
  impactBreakdown?: Array<{ entity?: string; entityName?: string; axis?: string; amplitude?: number; hops?: number }>;
  chainImpacts?: Array<{
    displayName?: string;
    chainName?: string;
    affected?: number;
    total?: number;
    affectedNodes?: number;
    totalNodes?: number;
    affectedLegs?: number;
    legCount?: number;
    pathStressScore?: number;
    bottleneckLegIndex?: number;
    recoveryLatencyHours?: number;
    bottleneckSummary?: string;
    dominantFactors?: string[];
    flowRefs?: string[];
    zoneRefs?: string[];
    affectedZoneRefs?: string[];
  }>;
  seedFailures?: Array<{ seed?: string; reason?: string }>;
  alternatives?: Array<{ entity?: string; entityName?: string; description?: string; tradeOff?: string }>;
  dataGaps?: Array<{ description?: string; whyItMatters?: string }>;
  highlightedNodeIds?: string[];
  highlightedEdgeIds?: string[];
  tracePaths?: unknown[];
};

type SufficiencyReport = {
  score: number;
  sufficient: boolean;
  gaps: Array<{ description: string; whyItMatters?: string }>;
  recommendedFacets?: string[];
};

/* ─── Preset question hints ─── */
const PRESET_CATEGORIES = [
  {
    label: 'Shock',
    presets: [
      'What happens if Strait of Hormuz is disrupted?',
      'What happens if a key supplier stops shipping?',
    ],
  },
  {
    label: 'Downstream',
    presets: [
      'Who gets hit later, not first?',
      'Which remote factories or markets feel this next?',
    ],
  },
  {
    label: 'Response',
    presets: [
      'What should we do first if this starts happening?',
      'What fallback paths exist?',
    ],
  },
  {
    label: 'Policy',
    presets: [
      'What happens if VAT rises from 12%% to 16%%?',
      'How would a new restriction propagate through the graph?',
    ],
  },
];

const riskTone = (level?: string): { bg: string; fg: string; border: string } => {
  switch (String(level || '').toUpperCase()) {
    case 'CRITICAL':
      return { bg: 'rgba(220,38,38,0.12)', fg: '#ef4444', border: 'rgba(220,38,38,0.28)' };
    case 'HIGH':
      return { bg: 'rgba(245,158,11,0.12)', fg: '#f59e0b', border: 'rgba(245,158,11,0.28)' };
    case 'MODERATE':
      return { bg: 'rgba(59,130,246,0.12)', fg: '#60a5fa', border: 'rgba(59,130,246,0.24)' };
    default:
      return { bg: 'rgba(34,197,94,0.12)', fg: '#22c55e', border: 'rgba(34,197,94,0.24)' };
  }
};

const prettyHorizon = (value?: string): string => {
  const raw = String(value || '').trim();
  if (!raw) return 'unspecified';
  return raw.replace(/_/g, ' ');
};

export const QAPanel: React.FC<QAPanelProps> = ({
  lensId,
  onHighlightNodes,
  onHighlightEdges,
  onHighlightTracePaths,
  externalOpenTrigger,
}) => {
  const [question, setQuestion] = React.useState('');
  const [status, setStatus] = React.useState<QAStatus>('idle');
  const [answerText, setAnswerText] = React.useState('');
  const [answer, setAnswer] = React.useState<QAAnswer | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const abortRef = React.useRef<AbortController | null>(null);

  // Task 3: new state
  const [sufficiencyReport, setSufficiencyReport] = React.useState<SufficiencyReport | null>(null);
  const [scoutStatus, setScoutStatus] = React.useState<'idle' | 'running' | 'complete'>('idle');
  const [autoScout, setAutoScout] = React.useState(false);

  const [fillReport, setFillReport] = React.useState<{
    filledGaps?: Array<{ gap: { entityName?: string; axis?: string }; injectedAxis?: string; sources?: Array<{ url: string; title: string }> }>;
    failedGaps?: Array<{ gap: { entityName?: string; axis?: string }; reason?: string }>;
    totalQueries?: number;
    totalSignals?: number;
    sources?: Array<{ url: string; title: string }>;
  } | null>(null);

  // Command Palette state
  const [paletteOpen, setPaletteOpen] = React.useState(true);
  const [activeIndex, setActiveIndex] = React.useState(0);
  const inputRef = React.useRef<HTMLInputElement>(null);

  const hasAnswer = !!(answer || answerText);
  const isBusy =
    status === 'decomposing' ||
    status === 'simulating' ||
    status === 'synthesizing' ||
    status === 'sufficiency_check' ||
    status === 'scouting' ||
    status === 'filling_gaps';

  // Open palette by default when idle + no answer
  React.useEffect(() => {
    if (status === 'idle' && !hasAnswer && !error) {
      setPaletteOpen(true);
    }
  }, [status, hasAnswer, error]);

  // Focus input when palette opens
  React.useEffect(() => {
    if (paletteOpen) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [paletteOpen]);

  // External open trigger (e.g. Ctrl+K from parent)
  React.useEffect(() => {
    if (externalOpenTrigger && externalOpenTrigger > 0) {
      setPaletteOpen(true);
    }
  }, [externalOpenTrigger]);

  // Filter presets by query
  const filteredCategories = React.useMemo(() => {
    const q = question.trim().toLowerCase();
    if (!q) return PRESET_CATEGORIES;
    return PRESET_CATEGORIES.map((cat) => ({
      ...cat,
      presets: cat.presets.filter((p) => p.toLowerCase().includes(q)),
    })).filter((cat) => cat.presets.length > 0);
  }, [question]);

  const flatPresets = React.useMemo(
    () => filteredCategories.flatMap((c) => c.presets),
    [filteredCategories],
  );

  React.useEffect(() => {
    setActiveIndex((i) => Math.min(i, Math.max(0, flatPresets.length - 1)));
  }, [flatPresets]);

  const handleSubmit = React.useCallback(async () => {
    const q = question.trim();
    if (!q || isBusy) return;
    setError(null);
    setAnswer(null);
    setAnswerText('');
    setSufficiencyReport(null);
    setFillReport(null);
    setScoutStatus('idle');
    setPaletteOpen(false);
    abortRef.current = new AbortController();
    setStatus('decomposing');
    try {
      await api.postHorizonLensQAStream(
        lensId,
        q,
        {
          onStatus: (d) => {
            setStatus((d.status as QAStatus) || 'decomposing');
            if (d.error) setError(d.error);
          },
          onHighlights: (d) => {
            if (d.highlightedNodeIds?.length) onHighlightNodes?.(d.highlightedNodeIds);
            if (d.highlightedEdgeIds?.length) onHighlightEdges?.(d.highlightedEdgeIds);
            if (d.tracePaths?.length) onHighlightTracePaths?.(d.tracePaths);
          },
          onAnswerChunk: (d) => setAnswerText((prev) => prev + (d.text || '')),
          onAnswerComplete: (d) => {
            setAnswer(d as QAAnswer);
            setStatus('answered');
          },
          onSufficiencyReport: (d) => setSufficiencyReport(d),
          onScoutStarted: () => setScoutStatus('running'),
          onScoutProgress: () => { /* progress indicator is driven by scoutStatus */ },
          onScoutComplete: () => setScoutStatus('complete'),
          onSignalFillReport: (d) => setFillReport(d),
          onError: (err) => {
            setError(err.message);
            setStatus('failed');
          },
        },
        abortRef.current.signal,
        { autoScout },
      );
    } catch (e) {
      setError((e as Error).message);
      setStatus('failed');
    } finally {
      abortRef.current = null;
    }
  }, [lensId, question, isBusy, autoScout, onHighlightNodes, onHighlightEdges, onHighlightTracePaths]);

  const statusLabel = (s: QAStatus) => {
    switch (s) {
      case 'decomposing': return 'Mapping the shock and the exposed pathways…';
      case 'simulating': return 'Estimating who gets hit first and who gets hit later…';
      case 'synthesizing': return 'Writing the decision brief…';
      case 'sufficiency_check': return 'Checking whether the evidence is strong enough…';
      case 'scouting': return 'Pulling more evidence where the graph is thin…';
      case 'filling_gaps': return 'Filling missing signals before answering…';
      default: return '';
    }
  };

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        flex: 1,
        minHeight: 0,
        padding: 24,
        gap: 20,
      }}
    >
      <style>{`
        @keyframes qa-palette-rise {
          from { opacity: 0; transform: translateY(8px); }
          to { opacity: 1; transform: translateY(0); }
        }
      `}</style>

      {/* ─── Command Palette (inline) ─── */}
      {paletteOpen && (
        <div
          style={{
            flex: isBusy || hasAnswer || error ? undefined : 1,
            display: 'flex',
            alignItems: isBusy || hasAnswer || error ? undefined : 'center',
            justifyContent: 'center',
            minHeight: 0,
          }}
        >
          <div
            style={{
              width: 520,
              maxWidth: '100%',
              maxHeight: '60vh',
              display: 'flex',
              flexDirection: 'column',
              background: 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 16,
              boxShadow: '0 8px 32px rgba(0,0,0,0.25)',
              overflow: 'hidden',
              animation: 'qa-palette-rise 0.18s ease-out',
            }}
          >
            {/* Search row */}
            <div style={{ display: 'flex', alignItems: 'center', padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ marginRight: 12, color: 'var(--text-secondary-strong)', display: 'flex' }}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
              </div>
              <input
                ref={inputRef}
                value={question}
                onChange={(e) => { setQuestion(e.target.value); setActiveIndex(0); }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    if (hasAnswer || error) setPaletteOpen(false);
                    return;
                  }
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    setActiveIndex((i) => Math.min(i + 1, Math.max(0, flatPresets.length - 1)));
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    setActiveIndex((i) => Math.max(0, i - 1));
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    if (flatPresets.length > 0 && !question.trim()) {
                      setQuestion(flatPresets[activeIndex]);
                    } else {
                      void handleSubmit();
                    }
                    return;
                  }
                }}
                placeholder="Ask what happens if..."
                style={{
                  flex: 1,
                  border: 'none',
                  outline: 'none',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 15,
                  fontWeight: 500,
                }}
              />
              {question.trim() && (
                <button
                  type="button"
                  onClick={() => void handleSubmit()}
                  style={{
                    marginLeft: 8,
                    padding: '6px 14px',
                    borderRadius: 8,
                    border: 'none',
                    background: 'var(--accent-primary, #3B82F6)',
                    color: 'white',
                    fontSize: 13,
                    fontWeight: 600,
                    cursor: 'pointer',
                  }}
                >
                  Ask
                </button>
              )}
            </div>

            {/* Auto-scout toggle */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                <input type="checkbox" checked={autoScout} onChange={(e) => setAutoScout(e.target.checked)} style={{ accentColor: 'var(--accent-primary, #3B82F6)' }} />
                Pull extra evidence if the answer is weak
              </label>
            </div>

            {/* Preset hints */}
            <div style={{ overflow: 'auto', flex: 1, minHeight: 0 }}>
              {filteredCategories.map((cat) => (
                <div key={cat.label}>
                  <div style={{ padding: '10px 16px 4px', fontSize: 11, fontWeight: 700, textTransform: 'uppercase', letterSpacing: 0.5, color: 'var(--text-secondary-strong)', opacity: 0.7 }}>
                    {cat.label}
                  </div>
                  {cat.presets.map((preset) => {
                    const idx = flatPresets.indexOf(preset);
                    const isActive = idx === activeIndex;
                    return (
                      <button
                        key={preset}
                        type="button"
                        onMouseEnter={() => setActiveIndex(idx)}
                        onClick={() => { setQuestion(preset); inputRef.current?.focus(); }}
                        style={{
                          display: 'block',
                          width: '100%',
                          textAlign: 'left',
                          padding: '10px 16px',
                          border: 'none',
                          background: isActive ? 'var(--white-10, rgba(255,255,255,0.06))' : 'transparent',
                          color: 'var(--text-heading)',
                          fontSize: 13,
                          cursor: 'pointer',
                        }}
                      >
                        {preset}
                      </button>
                    );
                  })}
                </div>
              ))}
              {filteredCategories.length === 0 && question.trim() && (
                <div style={{ padding: '16px', fontSize: 13, color: 'var(--text-secondary-strong)', textAlign: 'center' }}>
                  Press Enter to ask your question
                </div>
              )}
            </div>

            {/* Keyboard hint */}
            <div style={{ padding: '8px 16px', borderTop: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-secondary-strong)', opacity: 0.6 }}>
              <span>↑↓ Navigate</span>
              <span>Enter Submit{hasAnswer || error ? ' · Esc Close' : ''}</span>
            </div>
          </div>
        </div>
      )}

      {/* ─── Status / Progress ─── */}
      {isBusy && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ fontSize: 14, color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ display: 'inline-flex', gap: 4 }}>
              {[0, 1, 2].map((i) => (
                <span
                  key={i}
                  style={{
                    display: 'inline-block',
                    width: 6,
                    height: 6,
                    borderRadius: 999,
                    background: 'var(--accent-primary, #3B82F6)',
                    animation: 'qa-dot-bounce 1.1s infinite ease-in-out',
                    animationDelay: `${i * 0.14}s`,
                  }}
                />
              ))}
            </span>
            <style>{`
              @keyframes qa-dot-bounce {
                0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                40% { transform: translateY(-4px); opacity: 1; }
              }
            `}</style>
            {statusLabel(status)}
          </div>

          {/* Scout progress */}
          {scoutStatus === 'running' && (
            <div style={{ padding: 12, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)', fontSize: 13, color: 'var(--text-secondary-strong)' }}>
              Scouting for additional data…
            </div>
          )}
        </div>
      )}

      {/* ─── Sufficiency Report ─── */}
      {sufficiencyReport && !hasAnswer && (
        <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-heading)' }}>
              Evidence strength
            </div>
            <div style={{ flex: 1, height: 6, borderRadius: 3, background: 'var(--border-subtle)', overflow: 'hidden' }}>
              <div
                style={{
                  width: `${Math.round(sufficiencyReport.score * 100)}%`,
                  height: '100%',
                  borderRadius: 3,
                  background: sufficiencyReport.sufficient ? '#22C55E' : sufficiencyReport.score >= 0.5 ? '#F59E0B' : '#EF4444',
                  transition: 'width 0.3s ease',
                }}
              />
            </div>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary-strong)' }}>
              {Math.round(sufficiencyReport.score * 100)}%
            </div>
          </div>
          {sufficiencyReport.gaps.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 12 }}>
              {sufficiencyReport.gaps.map((g, i) => (
                <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  • {g.description}
                  {g.whyItMatters && <span style={{ opacity: 0.7 }}> — {g.whyItMatters}</span>}
                </div>
              ))}
            </div>
          )}
          {!sufficiencyReport.sufficient && scoutStatus === 'idle' && (
            <button
              type="button"
              onClick={() => { setAutoScout(true); void handleSubmit(); }}
              style={{
                padding: '8px 16px',
                borderRadius: 8,
                border: 'none',
                background: 'var(--accent-primary, #3B82F6)',
                color: 'white',
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Pull more evidence
            </button>
          )}
        </div>
      )}

      {/* ─── Signal Fill / Enrichment Report ─── */}
      {fillReport && (
        <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 12 }}>
            Added {fillReport.filledGaps?.length ?? 0} missing signal{(fillReport.filledGaps?.length ?? 0) !== 1 ? 's' : ''} from {fillReport.sources?.length ?? 0} source{(fillReport.sources?.length ?? 0) !== 1 ? 's' : ''}
          </div>

          {fillReport.filledGaps && fillReport.filledGaps.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 10 }}>
              {fillReport.filledGaps.map((fg, i) => (
                <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'baseline', gap: 6 }}>
                  <span style={{ color: '#22C55E', fontSize: 10 }}>●</span>
                  <span>
                    <strong style={{ fontWeight: 600, color: 'var(--text-heading)' }}>{fg.gap.entityName || '?'}</strong>
                    {fg.gap.axis && <span> — {fg.gap.axis}</span>}
                    {fg.sources && fg.sources.length > 0 && (
                      <>
                        {' · '}
                        <a
                          href={fg.sources[0].url}
                          target="_blank"
                          rel="noopener noreferrer"
                          style={{ color: 'var(--accent-primary, #3B82F6)', textDecoration: 'none' }}
                        >
                          {fg.sources[0].title || fg.sources[0].url}
                        </a>
                      </>
                    )}
                  </span>
                </div>
              ))}
            </div>
          )}

          {fillReport.failedGaps && fillReport.failedGaps.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 10 }}>
              {fillReport.failedGaps.map((fg, i) => (
                <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary-strong)', opacity: 0.55, display: 'flex', alignItems: 'baseline', gap: 6 }}>
                  <span style={{ fontSize: 10 }}>○</span>
                  <span>
                    {fg.gap.entityName || '?'}{fg.gap.axis && ` — ${fg.gap.axis}`}
                    {fg.reason && <span style={{ marginLeft: 6, fontStyle: 'italic' }}>({fg.reason})</span>}
                  </span>
                </div>
              ))}
            </div>
          )}

          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', opacity: 0.7 }}>
            This evidence is now available for later questions.
          </div>
        </div>
      )}

      {/* Error */}
      {error && (
        <div style={{ padding: 12, borderRadius: 12, background: 'rgba(220,38,38,0.15)', color: '#f87171', fontSize: 14 }}>
          {error}
        </div>
      )}

      {/* ─── Answer ─── */}
      {hasAnswer && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 16,
            flex: 1,
            minHeight: 0,
            overflow: 'auto',
          }}
        >
          {/* Headline */}
          <div
            style={{
              padding: 18,
              borderRadius: 14,
              border: `1px solid ${riskTone(answer?.riskLevel).border}`,
              background: riskTone(answer?.riskLevel).bg,
              display: 'flex',
              flexDirection: 'column',
              gap: 10,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
              {answer?.riskLevel && (
                <span
                  style={{
                    padding: '4px 10px',
                    borderRadius: 999,
                    fontSize: 11,
                    fontWeight: 800,
                    letterSpacing: '0.3px',
                    color: riskTone(answer?.riskLevel).fg,
                    border: `1px solid ${riskTone(answer?.riskLevel).border}`,
                  }}
                >
                  {answer.riskLevel}
                </span>
              )}
              {answer?.timeHorizon && (
                <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  {prettyHorizon(answer.timeHorizon)}
                </span>
              )}
              {answer && answer.confidence > 0 && (
                <span style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  confidence {Math.round(answer.confidence * 100)}%
                </span>
              )}
            </div>
            <div style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-heading)', lineHeight: 1.35 }}>
              {answer?.executiveSummary || answer?.text || answerText}
            </div>
            {answer?.riskScenario && (
              <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                {answer.riskScenario}
              </div>
            )}
          </div>

          {answer?.text && answer.executiveSummary && answer.text !== answer.executiveSummary && (
            <div style={{ fontSize: 14, color: 'var(--text-secondary-strong)', lineHeight: 1.6 }}>
              {answer.text}
            </div>
          )}

          {answer?.chainImpacts && answer.chainImpacts.length > 0 && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
              {answer.chainImpacts.map((c, i) => (
                <div
                  key={i}
                  style={{
                    padding: '12px 14px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                  }}
                >
                  <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-heading)', marginBottom: 4 }}>
                    {c.displayName || c.chainName || `Chain ${i + 1}`}
                  </div>
                  <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' }}>
                    <div style={{ fontSize: 20, fontWeight: 700, color: 'var(--accent-primary, #3B82F6)' }}>
                      {Math.round(((c.pathStressScore ?? 0) * 100))}%
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>path stress</div>
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
                    {(c.affectedNodes ?? c.affected ?? 0)}/{(c.totalNodes ?? c.total ?? 0)} nodes · {(c.affectedLegs ?? 0)}/{(c.legCount ?? 0)} legs
                  </div>
                  {c.bottleneckSummary && (
                    <div style={{ fontSize: 12, color: 'var(--text-heading)', marginTop: 8, fontWeight: 600 }}>
                      Bottleneck: {c.bottleneckSummary}
                    </div>
                  )}
                  {typeof c.recoveryLatencyHours === 'number' && c.recoveryLatencyHours > 0 && (
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
                      Recovery ≈ {Math.round(c.recoveryLatencyHours)}h
                    </div>
                  )}
                  {c.dominantFactors && c.dominantFactors.length > 0 && (
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 8 }}>
                      {c.dominantFactors.slice(0, 3).map((factor) => (
                        <span
                          key={factor}
                          style={{
                            padding: '3px 7px',
                            borderRadius: 999,
                            fontSize: 10,
                            color: 'var(--text-secondary-strong)',
                            border: '1px solid var(--border-subtle)',
                          }}
                        >
                          {factor}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {((answer?.firstOrderEffects && answer.firstOrderEffects.length > 0) || (answer?.secondOrderEffects && answer.secondOrderEffects.length > 0)) && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: 12 }}>
              {answer?.firstOrderEffects && answer.firstOrderEffects.length > 0 && (
                <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
                  <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-heading)' }}>
                    What changes first
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {answer.firstOrderEffects.map((item, i) => (
                      <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                          {item.severity && <span style={{ fontSize: 10, fontWeight: 700, color: riskTone(item.severity).fg }}>{item.severity}</span>}
                          {item.horizon && <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{prettyHorizon(item.horizon)}</span>}
                        </div>
                        <div style={{ fontSize: 13, color: 'var(--text-heading)', fontWeight: 600 }}>{item.summary}</div>
                        {item.mechanism && <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{item.mechanism}</div>}
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {answer?.secondOrderEffects && answer.secondOrderEffects.length > 0 && (
                <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
                  <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-heading)' }}>
                    What spreads later
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                    {answer.secondOrderEffects.map((item, i) => (
                      <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                          {item.severity && <span style={{ fontSize: 10, fontWeight: 700, color: riskTone(item.severity).fg }}>{item.severity}</span>}
                          {item.horizon && <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{prettyHorizon(item.horizon)}</span>}
                        </div>
                        <div style={{ fontSize: 13, color: 'var(--text-heading)', fontWeight: 600 }}>{item.summary}</div>
                        {item.mechanism && <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{item.mechanism}</div>}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {answer?.watchpoints && answer.watchpoints.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-heading)' }}>
                What to monitor next
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {answer.watchpoints.map((wp, i) => (
                  <div key={i} style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                    <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-heading)' }}>{wp.signal}</div>
                    {wp.whyItMatters && <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{wp.whyItMatters}</div>}
                    {wp.expectedWithin && <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{prettyHorizon(wp.expectedWithin)}</div>}
                  </div>
                ))}
              </div>
            </div>
          )}

          {answer?.recommendations && answer.recommendations.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-heading)' }}>
                What to do now
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {answer.recommendations.map((item, i) => (
                  <div key={i} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    {i + 1}. {item}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Alternatives */}
          {answer?.alternatives && answer.alternatives.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                Fallbacks and alternatives
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {answer.alternatives.map((a, i) => (
                  <div key={i} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    {a.entityName || a.entity}: {a.description}
                    {a.tradeOff && ` (${a.tradeOff})`}
                  </div>
                ))}
              </div>
            </div>
          )}

          {((answer?.assumptions && answer.assumptions.length > 0) || (answer?.dataGaps && answer.dataGaps.length > 0)) && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 700, marginBottom: 12, color: 'var(--text-heading)' }}>
                What could change this view
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {(answer?.assumptions || []).map((item, i) => (
                  <div key={`assumption-${i}`} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    • {item}
                  </div>
                ))}
                {(answer?.dataGaps || []).map((g, i) => (
                  <div key={`gap-${i}`} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    • {g.description}
                    {g.whyItMatters && ` — ${g.whyItMatters}`}
                  </div>
                ))}
              </div>
            </div>
          )}

          <details style={{ borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)', padding: 16 }}>
            <summary style={{ cursor: 'pointer', fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
              Diagnostics
            </summary>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16, marginTop: 14 }}>
              {answer?.seedFailures && answer.seedFailures.length > 0 && (
                <div style={{ padding: 14, borderRadius: 12, border: '1px solid rgba(234,179,8,0.4)', background: 'rgba(234,179,8,0.08)' }}>
                  <div style={{ fontSize: 13, fontWeight: 600, color: '#EAB308', marginBottom: 8 }}>
                    Some seeds could not be resolved
                  </div>
                  {answer.seedFailures.map((f, i) => (
                    <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                      {f.seed}: {f.reason}
                    </div>
                  ))}
                </div>
              )}

              {answer?.impactBreakdown && answer.impactBreakdown.length > 0 && (
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                    Propagation detail
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {answer.impactBreakdown.slice(0, 10).map((item, i) => (
                      <div key={i} style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                        {item.entityName || item.entity} — {item.axis} ({(item.amplitude ?? 0).toFixed(2)}, {item.hops ?? 0} hops)
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {answer?.evidence && answer.evidence.length > 0 && (
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                    Evidence
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {answer.evidence.map((ev, i) => (
                      <div
                        key={i}
                        style={{
                          fontSize: 12,
                          color: 'var(--text-secondary-strong)',
                          opacity: ev.verified === false ? 0.5 : 1,
                          display: 'flex',
                          alignItems: 'flex-start',
                          gap: 6,
                        }}
                      >
                        <span style={{ fontSize: 10, marginTop: 2 }}>
                          {ev.verified === false ? '○' : '●'}
                        </span>
                        <span>
                          {ev.summary || ev.canonicalPath}
                          {ev.confidence != null && ` (${Math.round(ev.confidence * 100)}%)`}
                          {ev.verified === false && <span style={{ color: '#F59E0B', marginLeft: 4 }}>unverified</span>}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </details>

          {/* Ask another question */}
          <button
            type="button"
            onClick={() => { setQuestion(''); setPaletteOpen(true); }}
            style={{
              alignSelf: 'flex-start',
              padding: '10px 18px',
              borderRadius: 10,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-heading)',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Ask another question
          </button>
        </div>
      )}

      {/* Empty state — palette closed, nothing to show */}
      {!hasAnswer && !isBusy && !error && !paletteOpen && !sufficiencyReport && (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', cursor: 'pointer' }} onClick={() => setPaletteOpen(true)}>
          <div style={{ fontSize: 14, color: 'var(--text-secondary-strong)', opacity: 0.6 }}>
            Click here or press <kbd style={{ padding: '2px 6px', borderRadius: 4, border: '1px solid var(--border-subtle)', fontSize: 12 }}>Enter</kbd> to ask a question
          </div>
        </div>
      )}
    </div>
  );
};
