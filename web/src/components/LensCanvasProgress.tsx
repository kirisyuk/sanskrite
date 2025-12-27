import React from 'react';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import AssembleLensRequestComponent, { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';

export interface LensCanvasProgressProps {
  lensId: string | null;
  projectId?: string | null;
  showInput?: boolean;
  onAssemble?: (request: AssembleLensRequest) => void;
  isCreatingLens?: boolean;
}

const getIcon = (icon?: string) => {
  switch (icon) {
    case 'assembly':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10" />
          <path d="M12 6v6l4 2" />
        </svg>
      );
    case 'scout':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z" />
          <circle cx="12" cy="10" r="3" />
        </svg>
      );
    case 'node':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10" />
          <circle cx="12" cy="12" r="4" />
        </svg>
      );
    case 'edge':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M5 12h14" />
          <path d="M12 5l7 7-7 7" />
        </svg>
      );
    case 'propagation':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
        </svg>
      );
    case 'success':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: '#2ECC71' }}>
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
          <polyline points="22 4 12 14.01 9 11.01" />
        </svg>
      );
    case 'error':
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: '#f87171' }}>
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
      );
    default:
      return (
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </svg>
      );
  }
};

export const LensCanvasProgress: React.FC<LensCanvasProgressProps> = ({ 
  lensId, 
  projectId,
  showInput = false,
  onAssemble,
  isCreatingLens = false,
}) => {
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

  const handleAssemble = React.useCallback((request: AssembleLensRequest) => {
    if (onAssemble) {
      onAssemble(request);
    }
  }, [onAssemble]);

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        minHeight: 0,
      }}
    >
      {/* Events feed */}
      <div
        ref={feedRef}
        style={{
          flex: showInput ? 1 : 'none',
          minHeight: showInput ? 0 : 'auto',
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
                padding: '12px 14px',
                borderRadius: 10,
                border: '1px solid var(--overlay-line-90)',
                background: 'var(--bg-elevated)',
                color: 'var(--text-secondary-strong)',
                fontSize: 13,
                marginBottom: 8,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
                {/* Icon */}
                <div style={{ 
                  flexShrink: 0, 
                  marginTop: 2,
                  color: ev.icon === 'success' ? '#2ECC71' : ev.icon === 'error' ? '#f87171' : 'var(--text-secondary-strong)'
                }}>
                  {getIcon(ev.icon)}
                </div>
                
                {/* Content */}
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
                    <div style={{ fontWeight: 600, color: 'var(--text-heading)', fontSize: 13 }}>
                      {ev.humanReadable?.title || ev.text}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-tertiary)', flexShrink: 0 }}>
                      {new Date(ev.ts).toLocaleTimeString()}
                    </div>
                  </div>
                  {(ev.humanReadable?.description || ev.humanReadable?.entityName) && (
                    <div style={{ marginTop: 6, fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.4 }}>
                      {ev.humanReadable.description}
                      {ev.humanReadable.entityName && !ev.humanReadable.description && (
                        <span style={{ fontStyle: 'italic' }}>{ev.humanReadable.entityName}</span>
                      )}
                    </div>
                  )}
                  {ev.humanReadable?.description && ev.humanReadable?.entityName && (
                    <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-tertiary)', fontFamily: 'monospace' }}>
                      {ev.data?.canonicalPath || ev.data?.targetCanonicalPath || ''}
                    </div>
                  )}
                </div>
              </div>
            </div>
          ))
        ) : (
          <div className="flex items-center justify-center py-8">
            <div className="text-xs text-center" style={{ color: 'var(--text-secondary-strong)' }}>
              {showInput 
                ? 'No events yet. Start assembling a lens to see progress here.'
                : 'No events yet. Events will appear here when lens assembly or scouting is in progress.'}
            </div>
          </div>
        )}
      </div>

      {/* Input field at bottom */}
      {showInput && projectId && (
        <div style={{ flexShrink: 0, paddingTop: 16 }}>
          <AssembleLensRequestComponent
            projectId={projectId}
            onAssemble={handleAssemble}
            disabled={isCreatingLens}
            pinnedBottom={true}
          />
        </div>
      )}
    </div>
  );
};
