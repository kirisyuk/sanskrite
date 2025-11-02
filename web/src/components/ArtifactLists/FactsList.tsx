import React from 'react';
import { api } from '../../lib/api';

export const FactsList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ statement: string; confidence?: number; evidence?: string[] }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const fdoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'facts.json');
      if (!fdoc?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, fdoc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.facts) ? json.facts : [];
      setItems(arr);
    } catch (e: any) {
      setError('Failed to load facts');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);
  const count = items.length;
  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M3 4h18v4H3zM3 10h18v10H3z"/><path d="M6 13h6M6 17h10" strokeLinecap="round"/></svg>
        <span className="text-[11px] font-medium">Facts ({count})</span>
      </div>
      <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ transform: open ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform 0.15s ease' }}><path d="M9 6l6 6-6 6"/></svg>
    </button>
  );
  if (loading) return <div className="text-11" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
  if (error) return <div className="text-11" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
  return (
    <div className="space-y-1">
      {header}
      {open && (
        <div className="grid" style={{ gridTemplateColumns: '1fr', gap: 6 }}>
          {items.length === 0 && (
            <div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No facts yet. Run "analyze facts".</div>
          )}
          {items.map((f, i) => (
            <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}>
              <div className="flex items-center justify-between gap-2">
                <div className="text-12" style={{ color: 'var(--text-heading)' }}>{f.statement}</div>
                {typeof f.confidence === 'number' && (
                  <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{Math.round(Math.max(0, Math.min(1, f.confidence))*100)}%</span>
                )}
              </div>
              {Array.isArray(f.evidence) && f.evidence.length > 0 && (
                <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>Evidence: {f.evidence.join('; ')}</div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

