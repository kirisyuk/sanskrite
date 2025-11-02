import React from 'react';
import { api } from '../../lib/api';

export const EventsList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ ts: string; title: string; summary?: string }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const edoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'events.json');
      if (!edoc?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, edoc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.events) ? json.events : [];
      setItems(arr);
    } catch (e: any) {
      setError('Failed to load events');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);
  const count = items.length;
  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 8h18M7 2v4M17 2v4"/></svg>
        <span className="text-[11px] font-medium">Events ({count})</span>
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
            <div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No events yet. Run "analyze events".</div>
          )}
          {items.map((e, i) => (
            <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}>
              <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{e.title}</div>
              <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{e.ts}</div>
              {e.summary && <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{e.summary}</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

