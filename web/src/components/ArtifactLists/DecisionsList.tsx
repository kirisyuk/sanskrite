import React from 'react';
import { api } from '../../lib/api';

export const DecisionsList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ title: string; selected?: string; rationale?: string }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const ddoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'decisions.json');
      if (!ddoc?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, ddoc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.decisions) ? json.decisions : [];
      setItems(arr);
    } catch (e: any) {
      setError('Failed to load decisions');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);
  const count = items.length;
  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M4 12h8M12 12l4-4M12 12l4 4" strokeLinecap="round" strokeLinejoin="round"/><circle cx="4" cy="12" r="2"/></svg>
        <span className="text-[11px] font-medium">Decisions ({count})</span>
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
            <div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No decisions yet. Run "analyze decisions".</div>
          )}
          {items.map((d, i) => (
            <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}>
              <div className="flex items-center justify-between gap-2">
                <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{d.title}</div>
                {d.selected && (
                  <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{d.selected}</span>
                )}
              </div>
              {d.rationale && <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{d.rationale}</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

