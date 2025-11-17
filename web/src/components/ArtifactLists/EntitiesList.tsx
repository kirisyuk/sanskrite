import React from 'react';
import { api } from '../../lib/api';

export const EntitiesList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ id?: string; name: string; type: string; description?: string; excluded?: boolean }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const ents = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'entities.json');
      if (!ents?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, ents.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.entities) ? json.entities : [];
      setItems(arr.filter((e: any) => !e?.excluded));
    } catch (e: any) {
      setError('Failed to load entities');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);

  const count = items.length;

  const typeIcon = (t: string) => {
    const stroke = 'currentColor';
    const normalizedType = t.toLowerCase();
    // Support both new ontology types and legacy types for backward compatibility
    switch (normalizedType) {
      // New ontology types
      case 'person':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><circle cx="12" cy="8" r="3"/><path d="M4 20c0-3.314 3.134-6 7-6s7 2.686 7 6"/></svg>);
      case 'organization':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/><circle cx="12" cy="7" r="1"/><path d="M7.5 10l2.5 1.45v2.9L7.5 16.5"/><path d="M16.5 10L14 11.45v2.9l2.5 1.45"/><path d="M16.5 16.5l-2.5-1.45v-2.9l2.5-1.45"/></svg>);
      case 'product':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>);
      case 'location':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>);
      case 'document':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M7 3h10l5 5v13H7z"/><path d="M7 3v7h7"/><path d="M9 13h6M9 17h6"/></svg>);
      case 'facility':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="4" y="3" width="16" height="16" rx="1"/><path d="M4 7h16M8 11h8M8 15h6"/><path d="M8 3v4M16 3v4"/></svg>);
      // Legacy types (for backward compatibility)
      case 'organization_or_platform':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/><circle cx="12" cy="7" r="1"/><path d="M7.5 10l2.5 1.45v2.9L7.5 16.5"/><path d="M16.5 10L14 11.45v2.9l2.5 1.45"/><path d="M16.5 16.5l-2.5-1.45v-2.9l2.5-1.45"/></svg>);
      case 'project_or_product':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>);
      case 'classifier_or_standard':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 10h3M7 14h3M7 7h6"/><circle cx="15.5" cy="12.5" r="2.5"/></svg>);
      case 'classification_code':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="5" y="4" width="14" height="16" rx="2"/><path d="M8 9h8M8 12h8M8 15h4"/></svg>);
      case 'regulatory_document':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M7 3h10l5 5v13H7z"/><path d="M7 3v7h7"/><path d="M9 13h6M9 17h6"/></svg>);
      case 'government_agency':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="4" y="3" width="16" height="16" rx="1"/><path d="M4 7h16M8 11h8M8 15h6"/><path d="M8 3v4M16 3v4"/></svg>);
      case 'contract_document':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M6 2h12v20H6z"/><path d="M9 7h6M9 11h6M9 15h4"/><path d="M6 2l3 6-3 6M18 2l-3 6 3 6"/></svg>);
      default:
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><circle cx="12" cy="12" r="9"/></svg>);
    }
  };

  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="4" y="5" width="16" height="4" rx="1"/><rect x="4" y="11" width="16" height="4" rx="1"/><rect x="4" y="17" width="16" height="2" rx="1"/></svg>
        <span className="text-[11px] font-medium">Entities ({count})</span>
      </div>
      <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ transform: open ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform 0.15s ease' }}><path d="M9 6l6 6-6 6"/></svg>
    </button>
  );

  if (loading) return <div className="text-11" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
  if (error) return <div className="text-11" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
  if (items.length === 0) return (
    <div className="space-y-1">
      {header}
      {open && (<div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No entities yet. Run "analyze entities".</div>)}
    </div>
  );

  return (
    <div className="space-y-1">
      {header}
      {open && (
        <div className="grid" style={{ gridTemplateColumns: '1fr', gap: 6 }}>
          {items.map((e, i) => {
            const key = `${e.name}-${e.type}-${i}`;
            const isExp = !!expanded[key];
            return (
              <button
                key={key}
                type="button"
                onClick={() => setExpanded((s) => ({ ...s, [key]: !isExp }))}
                className="p-2 rounded text-left w-full"
                style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}
              >
                <div className="flex items-center gap-2 justify-between">
                  <div className="flex items-center gap-2">
                    <span style={{ color: 'var(--text-secondary-strong)' }}>{typeIcon(e.type)}</span>
                    <span className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{e.name}</span>
                  </div>
                  <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{e.type}</span>
                </div>
                {isExp && e.description && (
                  <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{e.description}</div>
                )}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};

