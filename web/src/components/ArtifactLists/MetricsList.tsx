import React from 'react';
import { api } from '../../lib/api';

export const MetricsList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ key: string; label: string; value: any; unit?: string }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const mdoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'metrics.json');
      if (!mdoc?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, mdoc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.metrics) ? json.metrics : [];
      setItems(arr);
    } catch (e: any) {
      setError('Failed to load metrics');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);
  const count = items.length;
  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M3 20V4M9 20V8M15 20v-6M21 20H2"/></svg>
        <span className="text-[11px] font-medium">Metrics ({count})</span>
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
            <div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No metrics yet. Run "analyze metrics".</div>
          )}
          {items.map((m, i) => (
            <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}>
              <div className="flex items-center justify-between gap-2">
                <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{m.label || m.key}</div>
                <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{String(m.value)}{m.unit ? ` ${m.unit}` : ''}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

