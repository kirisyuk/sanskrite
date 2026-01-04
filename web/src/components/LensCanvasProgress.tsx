import React from 'react';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import AssembleLensRequestComponent, { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';

export interface LensCanvasProgressProps {
  lensId: string | null;
  projectId?: string | null;
  showInput?: boolean;
  onAssemble?: (request: AssembleLensRequest) => void;
  isCreatingLens?: boolean;
  /** When creating a new lens (lensId=null), filter progress events to only those matching this creation attempt. */
  activeCreateCorrelationId?: string | null;
  activeCreateSessionId?: string | null;
}

const formatEdgeType = (type: string) => {
  return (type || '').replace(/_/g, ' ').toLowerCase().replace(/\b\w/g, c => c.toUpperCase());
};

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
  activeCreateCorrelationId = null,
  activeCreateSessionId = null,
}) => {
  const events = useHorizonRealtimeStore((s) => s.events);
  const feedRef = React.useRef<HTMLDivElement | null>(null);

  // Filter events by lensId and projectId
  // Show events that match the lensId/projectId
  // If filtering by lensId, only show events with matching lensId (or no lensId if projectId matches)
  const filteredEvents = React.useMemo(() => {
    const base = (events || []).filter((ev) => {
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

    // New Lens is an "ultimate" user intent: do not show unrelated background jobs.
    // If no lens is selected:
    // - when NOT creating: show no events at all (keep the creation UX clean)
    // - when creating: show only events belonging to this creation attempt (correlation/session ID)
    if (!lensId) {
      if (!isCreatingLens) return [];
      const cid = String(activeCreateCorrelationId || '').trim();
      const sid = String(activeCreateSessionId || '').trim();
      if (!cid && !sid) return [];
      return base.filter((ev) => {
        const d: any = (ev as any)?.data;
        const evCid = typeof d?.correlationId === 'string' ? String(d.correlationId).trim() : '';
        const evSid = typeof d?.sessionId === 'string' ? String(d.sessionId).trim() : '';
        if (cid && evCid && evCid === cid) return true;
        if (sid && evSid && evSid === sid) return true;
        return false;
      });
    }

    return base;
  }, [events, lensId, projectId, isCreatingLens, activeCreateCorrelationId, activeCreateSessionId]);

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

  const hasEvents = filteredEvents.length > 0;
  // Shift input to bottom if we have events or creation is in progress
  const isContentActive = hasEvents || isCreatingLens;

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        flex: 1, 
        justifyContent: isContentActive ? 'flex-start' : 'center',
      }}
    >
      {/* Events feed */}
      {isContentActive && (
        <div
          ref={feedRef}
          style={{
            flex: 1,
            minHeight: 0,
            width: '100%',
            overflowY: 'auto',
            padding: '16px 4px', // Небольшой отступ
          }}
        >
          <div style={{ position: 'relative', paddingLeft: 36 }}>
            {/* Timeline line */}
            <div
              style={{
                position: 'absolute',
                left: 11, // Center of the icon (24px width -> 12px center).
                top: 4,
                bottom: 20, // Don't go all the way down
                width: 2,
                background: 'var(--border-subtle)',
                zIndex: 0,
              }}
            />

            {/* Initial Loading State */}
            {filteredEvents.length === 0 && isCreatingLens && (
              <div
                style={{
                  position: 'relative',
                  marginBottom: 16,
                  color: 'var(--text-secondary-strong)',
                  fontSize: 13,
                }}
              >
                <div
                  style={{
                    position: 'absolute',
                    left: -36,
                    top: 0,
                    width: 24,
                    height: 24,
                    borderRadius: '50%',
                    background: 'var(--bg-elevated)',
                    border: '2px solid var(--border-subtle)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    zIndex: 1,
                    color: 'var(--text-tertiary)',
                  }}
                >
                  <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <circle cx="12" cy="12" r="10" strokeOpacity="0.25" strokeWidth="3" />
                    <path d="M12 2a10 10 0 0 1 10 10" strokeOpacity="0.75" strokeWidth="3" />
                  </svg>
                </div>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontWeight: 600, color: 'var(--text-heading)', fontSize: 13, lineHeight: '20px' }}>
                    Initializing assembly...
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--text-tertiary)', fontFamily: 'monospace' }}>
                    Sending request
                  </div>
                </div>
              </div>
            )}

            {filteredEvents.map((ev) => {
              const isEdge = ev.icon === 'edge' || ev.type === 'horizon.graph.edge.upserted';
              let title = ev.humanReadable?.title || ev.text;
              let description = ev.humanReadable?.description;
              let entityPath = ev.data?.canonicalPath || ev.data?.targetCanonicalPath || '';
              let hasLowConfidence = false;

              // For edges, we'll render "Link <chip>Type</chip> created" separately
              // Store the edge type for chip rendering
              let edgeTypeForChip = '';
              if (isEdge) {
                const type = ev.data?.type || 'Connection';
                // Use humanReadable.title if available (already formatted), otherwise format ourselves
                edgeTypeForChip = ev.humanReadable?.title || formatEdgeType(type);
                title = ''; // We'll render custom layout for edges
                
                const s = ev.data?.qualifiers?.sourceCanonicalPath || ev.data?.sourceFragmentId || '';
                const t = ev.data?.qualifiers?.targetCanonicalPath || ev.data?.targetFragmentId || '';
                
                if (s && t) {
                   description = `${s} → ${t}`;
                } else {
                   description = 'Connection details';
                }
                entityPath = ''; // Don't show path for edge, description covers it
              } else {
                // Handle low_confidence in title
                if (title && title.includes('low_confidence')) {
                    title = title.replace('low_confidence', '').trim();
                    hasLowConfidence = true;
                }
                // Handle Unknown entity
                if (title && title.includes('Unknown entity') && entityPath) {
                    title = `Finished analyzing ${entityPath.split('/').pop()}`;
                }
              }

              return (
                <div
                  key={ev.id}
                  style={{
                    position: 'relative',
                    marginBottom: 16,
                    color: 'var(--text-secondary-strong)',
                    fontSize: 13,
                  }}
                >
                  {/* Icon Marker */}
                  <div
                    style={{
                      position: 'absolute',
                      left: -36,
                      top: 0,
                      width: 24,
                      height: 24,
                      borderRadius: '50%',
                      background: 'var(--bg-elevated)',
                      border: '2px solid var(--border-subtle)',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      zIndex: 1,
                      color: ev.icon === 'success' ? '#2ECC71' : ev.icon === 'error' ? '#f87171' : 'var(--text-tertiary)',
                    }}
                  >
                    {getIcon(ev.icon)}
                  </div>

                  {/* Content */}
                  <div style={{ minWidth: 0 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'baseline' }}>
                      <div style={{ fontWeight: 600, color: 'var(--text-heading)', fontSize: 13, lineHeight: '20px' }}>
                        {isEdge ? (
                          // Show "Link <chip>Type</chip> created" format
                          <>
                            <span style={{ fontWeight: 500, color: 'var(--text-secondary)' }}>Link </span>
                            <span 
                              style={{ 
                                display: 'inline-block', 
                                fontSize: 11, 
                                padding: '2px 8px', 
                                borderRadius: 4, 
                                background: 'var(--accent-primary-10, rgba(99, 102, 241, 0.1))', 
                                border: '1px solid var(--accent-primary-20, rgba(99, 102, 241, 0.2))',
                                color: 'var(--accent-primary, #6366f1)', 
                                fontWeight: 600,
                                verticalAlign: 'middle',
                              }}
                            >
                              {edgeTypeForChip}
                            </span>
                            <span style={{ fontWeight: 500, color: 'var(--text-secondary)' }}> created</span>
                          </>
                        ) : (
                          title
                        )}
                        {hasLowConfidence && (
                          <span 
                            style={{ 
                              display: 'inline-block', 
                              marginLeft: 8, 
                              fontSize: 10, 
                              padding: '1px 6px', 
                              borderRadius: 4, 
                              background: 'rgba(239, 68, 68, 0.1)', 
                              color: '#EF4444', 
                              fontWeight: 500,
                              verticalAlign: 'middle',
                            }}
                          >
                            Low Confidence
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: 11, color: 'var(--text-tertiary)', flexShrink: 0, fontFamily: 'monospace' }}>
                        {new Date(ev.ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                      </div>
                    </div>
                    
                    {(description || ev.humanReadable?.entityName) && (
                      <div style={{ marginTop: 2, fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.4 }}>
                        {description}
                        {ev.humanReadable?.entityName && !description && (
                          <span style={{ fontStyle: 'italic' }}>{ev.humanReadable?.entityName}</span>
                        )}
                      </div>
                    )}
                    
                    {entityPath && (
                      <div 
                        style={{ 
                          marginTop: 4, 
                          fontSize: 10, 
                          color: 'var(--text-tertiary)', 
                          fontFamily: 'monospace',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                          opacity: 0.7,
                        }}
                        title={entityPath}
                      >
                        {entityPath}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Input field */}
      {showInput && projectId && (
        <div style={{ 
          flexShrink: 0, 
          paddingTop: isContentActive ? 16 : 0,
          width: '100%',
          maxWidth: isContentActive ? '100%' : '600px',
          margin: isContentActive ? '0' : 'auto', // auto centers both vertically and horizontally in flex
          transition: 'all 0.3s ease', // Smooth transition if possible
        }}>
          <AssembleLensRequestComponent
            projectId={projectId}
            onAssemble={handleAssemble}
            disabled={isCreatingLens}
            pinnedBottom={isContentActive}
          />
        </div>
      )}
    </div>
  );
};
