import React from 'react';
import { useHorizonRealtimeStore, type HorizonRealtimeEvent } from '../stores/horizonRealtimeStore';

function isRelevant(ev: HorizonRealtimeEvent): boolean {
  const t = ev.type || '';
  if (t === 'horizon.graph.node.upserted') return true;
  if (t === 'horizon.graph.edge.upserted') return true;
  if (t === 'horizon.scout.job.state') return true;
  if (t === 'horizon.scout.job.completed') return true;
  if (t === 'horizon.canvas.newsByFragments.completed') return true;
  if (t.startsWith('horizon.domain.contract.')) return true;
  if (t.startsWith('horizon.intra_graph.')) return true;
  return false;
}

export const HorizonRealtimeCallouts: React.FC<{
  enabled: boolean;
  lensId?: string | null;
  projectId?: string | null;
}> = ({ enabled, lensId, projectId }) => {
  const events = useHorizonRealtimeStore((s) => s.events);
  const [items, setItems] = React.useState<Array<HorizonRealtimeEvent & { dismissAt: number }>>([]);
  const seenRef = React.useRef<Set<string>>(new Set());
  const MAX_SEEN_IDS = 1200;

  const pruneSeen = React.useCallback(() => {
    const seen = seenRef.current;
    if (!seen) return;
    if (seen.size <= MAX_SEEN_IDS) return;
    // Set preserves insertion order; drop oldest ids first.
    const over = seen.size - MAX_SEEN_IDS;
    let dropped = 0;
    for (const id of seen) {
      seen.delete(id);
      dropped += 1;
      if (dropped >= over) break;
    }
  }, []);

  // Prune timer
  React.useEffect(() => {
    if (!enabled) return;
    const id = window.setInterval(() => {
      const now = Date.now();
      setItems((prev) => prev.filter((x) => x.dismissAt > now).slice(-6));
    }, 500);
    return () => window.clearInterval(id);
  }, [enabled]);

  // Reset local caches when switching context / disabling (prevents unbounded growth over long sessions).
  React.useEffect(() => {
    if (!enabled) {
      setItems([]);
      seenRef.current.clear();
      return;
    }
    // On context change, we don't want "seen" to keep growing with unrelated ids.
    setItems([]);
    seenRef.current.clear();
  }, [enabled, lensId, projectId]);

  React.useEffect(() => {
    if (!enabled) return;
    const now = Date.now();

    const filtered = (events || []).filter((ev) => {
      if (!isRelevant(ev)) return false;
      if (projectId && ev.projectId && ev.projectId !== projectId) return false;
      if (lensId) {
        if (ev.lensId && ev.lensId !== lensId) return false;
        if (!ev.lensId) return false;
      }
      return true;
    });

    const tail = filtered.slice(-10);
    const nextToAdd: Array<HorizonRealtimeEvent & { dismissAt: number }> = [];
    for (const ev of tail) {
      if (!ev?.id) continue;
      if (seenRef.current.has(ev.id)) continue;
      seenRef.current.add(ev.id);
      nextToAdd.push({ ...ev, dismissAt: now + 6500 });
    }
    pruneSeen();
    if (nextToAdd.length === 0) return;

    setItems((prev) => [...prev, ...nextToAdd].slice(-6));
  }, [events, enabled, lensId, projectId, pruneSeen]);

  if (!enabled) return null;
  if (!items || items.length === 0) return null;

  return (
    <div
      style={{
        position: 'absolute',
        right: 16,
        bottom: 16,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        zIndex: 60,
        pointerEvents: 'none',
      }}
    >
      {items
        .slice()
        .reverse()
        .map((ev) => {
          const title = ev.humanReadable?.title || ev.text || ev.type;
          const desc = ev.humanReadable?.description;
          return (
            <div
              key={ev.id}
              style={{
                width: 320,
                maxWidth: 'min(320px, calc(100vw - 32px))',
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
                padding: '10px 12px',
                pointerEvents: 'none',
              }}
            >
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>
                {title}
              </div>
              {desc && (
                <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {desc}
                </div>
              )}
            </div>
          );
        })}
    </div>
  );
};


