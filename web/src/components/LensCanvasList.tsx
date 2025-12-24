import React from 'react';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';

export interface LensCanvasListProps {
  lensId: string | null;
  projectId?: string | null;
}

export const LensCanvasList: React.FC<LensCanvasListProps> = ({ lensId, projectId }) => {
  const events = useHorizonRealtimeStore((s) => s.events);
  const feedRef = React.useRef<HTMLDivElement | null>(null);

  // Filter events by lensId and projectId
  // Show events that match the lensId/projectId
  // If filtering by lensId, only show events with matching lensId (or no lensId if projectId matches)
  const filteredEvents = React.useMemo(() => {
    return (events || []).filter((ev) => {
      // If filtering by projectId, event must match projectId (or have no projectId)
      if (projectId) {
        if (ev.projectId && ev.projectId !== projectId) {
          return false;
        }
      }
      // If filtering by lensId, event must match lensId
      if (lensId) {
        if (ev.lensId && ev.lensId !== lensId) {
          return false;
        }
        // If we're filtering by lensId, don't show events without lensId
        if (!ev.lensId) {
          return false;
        }
      }
      return true;
    });
  }, [events, lensId, projectId]);

  // Auto-scroll to bottom when new events arrive
  React.useEffect(() => {
    const el = feedRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [filteredEvents.length]);

  return (
    <div
      ref={feedRef}
      style={{
        flex: 1,
        minHeight: 0,
        width: '100%',
        overflowY: 'auto',
        padding: '8px 0',
      }}
    >
      {filteredEvents.length > 0 ? (
        filteredEvents.map((ev) => (
          <div
            key={ev.id}
            style={{
              padding: '8px 10px',
              borderRadius: 10,
              border: '1px solid var(--overlay-line-90)',
              background: 'var(--bg-elevated)',
              color: 'var(--text-secondary-strong)',
              fontSize: 13,
              marginBottom: 8,
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
              <div style={{ fontWeight: 600, color: 'var(--text-heading)', fontSize: 12 }}>
                {ev.type}
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
                {new Date(ev.ts).toLocaleTimeString()}
              </div>
            </div>
            <div style={{ marginTop: 4, whiteSpace: 'pre-wrap' }}>{ev.text}</div>
          </div>
        ))
      ) : (
        <div className="flex items-center justify-center py-8">
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            No events yet. Events will appear here when lens assembly or scouting is in progress.
          </div>
        </div>
      )}
    </div>
  );
};

