import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Legend, Tooltip } from 'recharts';

// Lightweight visualization of the discussion flow (recent turns)
export const DiscussionFlow: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const genaiChat = useGenaiChatStore();
  const notifiedRef = React.useRef(false);

  React.useEffect(() => {
    // Notify availability once there are at least 2 turns
    const hasFlow = genaiChat.messages.length >= 2;
    if (hasFlow && !notifiedRef.current) {
      notifiedRef.current = true;
      onFirstResult && onFirstResult();
    }
  }, [genaiChat.messages.length, onFirstResult]);

  if (genaiChat.messages.length === 0) return null;
  const recent = genaiChat.messages.slice(-8);

  const trim = (s: string, n: number) => {
    const t = (s || '').trim();
    return t.length > n ? t.slice(0, n - 1) + '…' : t;
  };

  return (
    <div className="mt-3 p-3">
      <div className="text-13 mb-2" style={{ color: '#669999' }}>Discussion Flow:</div>
      <div className="space-y-1">
        {recent.map((m, i) => {
          const isUser = m.role === 'user';
          return (
            <div key={m.id} className="text-12 flex items-start gap-2" style={{ color: 'var(--text-secondary-strong)' }}>
              <span className="px-1.5 py-0.5 rounded text-[10px]" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>
                {isUser ? 'you' : 'assistant'}
              </span>
              <span style={{ whiteSpace: 'pre-wrap' }}>{trim(m.content, 220)}</span>
            </div>
          );
        })}
      </div>

      {/* Parsed @sx list moved to Artifacts panel */}

      {/* Bias levels chart (user vs assistant) via Recharts */}
      {(() => {
        const maxPoints = 10;
        const windowMsgs = genaiChat.messages.slice(-maxPoints);
        if (windowMsgs.length === 0) return null;
        const clamp01 = (x: any) => Math.max(0, Math.min(1, Number(x) || 0));
        const data = windowMsgs.map((m, idx) => {
          const ev = genaiChat.evaluations[m.id];
          return {
            idx,
            user: m.role === 'user' && ev ? clamp01(ev.biasLevel) : null,
            assistant: m.role === 'assistant' && ev ? clamp01(ev.biasLevel) : null,
          };
        });
        return (
          <div className="mt-3" style={{ border: '1px solid var(--overlay-line-90)', borderRadius: 8, background: 'var(--bg-elevated)' }}>
            <div className="text-[11px] mb-1 px-2 pt-2" style={{ color: 'var(--text-tertiary-weak)' }}>Bias (last messages)</div>
            <div className="px-2 pb-2" style={{ width: '100%' }}>
              <ResponsiveContainer width="100%" height={140}>
                <LineChart data={data} margin={{ top: 6, right: 12, bottom: 6, left: 6 }}>
                  <CartesianGrid stroke="var(--overlay-line-90)" strokeDasharray="3 3" />
                  <XAxis dataKey="idx" tick={false} axisLine={false} tickLine={false} />
                  <YAxis domain={[0, 1]} tickFormatter={(v: number | string) => `${Math.round(Number(v) * 100)}%`} width={28} stroke="var(--overlay-line-90)" />
                  <Tooltip formatter={(v: any) => (typeof v === 'number' ? `${Math.round(v * 100)}%` : '')} labelFormatter={() => ''} contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }} />
                  <Legend wrapperStyle={{ paddingTop: 0 }} />
                  <Line type="monotone" dataKey="user" name="you" stroke="var(--brand-25)" dot={false} isAnimationActive={false} connectNulls={false} />
                  <Line type="monotone" dataKey="assistant" name="assistant" stroke="var(--text-secondary-strong)" strokeDasharray="4 2" dot={false} isAnimationActive={false} connectNulls={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </div>
        );
      })()}
    </div>
  );
};

