/**
 * Lens Q&A — Analysis view (plan §10).
 * Question input, answer display, impact chain, evidence.
 */

import React from 'react';
import { api } from '../lib/api';

export interface QAPanelProps {
  lensId: string;
  projectId?: string;
  onHighlightNodes?: (nodeIds: string[]) => void;
  onHighlightTracePaths?: (paths: unknown[]) => void;
}

type QAStatus = 'idle' | 'decomposing' | 'simulating' | 'synthesizing' | 'answered' | 'failed';

type QAAnswer = {
  text: string;
  confidence: number;
  evidence?: Array<{ type?: string; canonicalPath?: string; summary?: string; confidence?: number }>;
  impactBreakdown?: Array<{ entity?: string; entityName?: string; axis?: string; amplitude?: number; hops?: number }>;
  alternatives?: Array<{ entity?: string; entityName?: string; description?: string; tradeOff?: string }>;
  dataGaps?: Array<{ description?: string; whyItMatters?: string }>;
  highlightedNodeIds?: string[];
  tracePaths?: unknown[];
};

export const QAPanel: React.FC<QAPanelProps> = ({
  lensId,
  onHighlightNodes,
  onHighlightTracePaths,
}) => {
  const [question, setQuestion] = React.useState('');
  const [status, setStatus] = React.useState<QAStatus>('idle');
  const [answerText, setAnswerText] = React.useState('');
  const [answer, setAnswer] = React.useState<QAAnswer | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const abortRef = React.useRef<AbortController | null>(null);

  const handleSubmit = React.useCallback(async () => {
    const q = question.trim();
    if (!q || status !== 'idle') return;
    setError(null);
    setAnswer(null);
    setAnswerText('');
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
            if (d.tracePaths?.length) onHighlightTracePaths?.(d.tracePaths);
          },
          onAnswerChunk: (d) => setAnswerText((prev) => prev + (d.text || '')),
          onAnswerComplete: (d) => {
            setAnswer(d as QAAnswer);
            setStatus('answered');
          },
          onError: (err) => {
            setError(err.message);
            setStatus('failed');
          },
        },
        abortRef.current.signal
      );
    } catch (e) {
      setError((e as Error).message);
      setStatus('failed');
    } finally {
      abortRef.current = null;
    }
  }, [lensId, question, status, onHighlightNodes, onHighlightTracePaths]);

  const isBusy = status === 'decomposing' || status === 'simulating' || status === 'synthesizing';

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
      {/* Question input — plan §10.2 */}
      <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
        <input
          type="text"
          placeholder="What do you want to know?"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleSubmit()}
          disabled={isBusy}
          style={{
            flex: 1,
            padding: '12px 16px',
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: 'var(--bg-elevated)',
            color: 'var(--text-heading)',
            fontSize: 14,
          }}
        />
        <button
          type="button"
          onClick={handleSubmit}
          disabled={isBusy || !question.trim()}
          style={{
            padding: '12px 20px',
            borderRadius: 12,
            border: 'none',
            background: isBusy ? 'var(--border-subtle)' : 'var(--accent-primary, #3B82F6)',
            color: 'white',
            fontWeight: 600,
            cursor: isBusy ? 'not-allowed' : 'pointer',
          }}
        >
          {isBusy ? '…' : 'Ask'}
        </button>
      </div>

      {/* Tracing indicator — plan §10.3 Stage 1 */}
      {isBusy && (
        <div style={{ fontSize: 14, color: 'var(--text-secondary-strong)' }}>
          ● ● ● {status === 'decomposing' && 'Understanding your question…'}
          {status === 'simulating' && 'Tracing impact through the graph…'}
          {status === 'synthesizing' && 'Generating answer…'}
        </div>
      )}

      {/* Error */}
      {error && (
        <div style={{ padding: 12, borderRadius: 12, background: 'rgba(220,38,38,0.15)', color: '#f87171', fontSize: 14 }}>
          {error}
        </div>
      )}

      {/* Answer — plan §10.3 Stage 2–3 */}
      {(answer || answerText) && (
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
          <div style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-heading)', lineHeight: 1.4 }}>
            {answer?.text || answerText}
          </div>

          {/* Confidence — plan §10.8 */}
          {answer && answer.confidence > 0 && (
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              {answer.confidence > 0.8 && '🟢 High confidence'}
              {answer.confidence >= 0.5 && answer.confidence <= 0.8 && '🟡 Moderate confidence — some data gaps'}
              {answer.confidence < 0.5 && answer.confidence > 0 && '🔴 Low confidence'}
            </div>
          )}

          {/* Impact breakdown */}
          {answer?.impactBreakdown && answer.impactBreakdown.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                How does it spread?
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {answer.impactBreakdown.slice(0, 10).map((item, i) => (
                  <div key={i} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    {item.entityName || item.entity} — {item.axis} ({(item.amplitude ?? 0).toFixed(2)}, {item.hops ?? 0} hops)
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Alternatives */}
          {answer?.alternatives && answer.alternatives.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                What can you do?
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

          {/* Data gaps — plan §10.9 */}
          {answer?.dataGaps && answer.dataGaps.length > 0 && (
            <div style={{ padding: 16, borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--bg-elevated)' }}>
              <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-heading)' }}>
                What don&apos;t we know?
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {answer.dataGaps.map((g, i) => (
                  <div key={i} style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                    {g.description}
                    {g.whyItMatters && ` — ${g.whyItMatters}`}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Empty state */}
      {!answer && !answerText && !isBusy && !error && (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-secondary-strong)', fontSize: 14 }}>
          Ask a question about your lens — impact, risks, alternatives, or dependencies.
        </div>
      )}
    </div>
  );
};
