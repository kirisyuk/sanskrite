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
  /** Optional preflight UI (inlined). */
  preflight?: {
    active: boolean;
    reason?: string;
    mode: 'just_open' | 'research_first';
    setMode: (mode: 'just_open' | 'research_first') => void;
    focus: string;
    setFocus: (focus: string) => void;
    domainsDraft: any[];
    setDomainsDraft: (next: any[]) => void;
    anchorsDraft: any[];
    setAnchorsDraft: (next: any[]) => void;
    selectedDomains: Record<string, boolean>;
    setSelectedDomains: (next: Record<string, boolean> | ((prev: Record<string, boolean>) => Record<string, boolean>)) => void;
    selectedAnchors: Record<string, boolean>;
    setSelectedAnchors: (next: Record<string, boolean> | ((prev: Record<string, boolean>) => Record<string, boolean>)) => void;
    isBusy: boolean;
    isValidCanonicalPath: (cp: string) => boolean;
    onCancel: () => void;
    onContinue: (mode?: 'just_open' | 'research_first') => void;
  } | null;
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
  preflight = null,
}) => {
  const events = useHorizonRealtimeStore((s) => s.events);
  const feedRef = React.useRef<HTMLDivElement | null>(null);
  const pinnedToBottomRef = React.useRef<boolean>(true);
  const [pinnedToBottom, setPinnedToBottom] = React.useState<boolean>(true);

  const preflightActive = !!(preflight && preflight.active);

  const [preflightExpanded, setPreflightExpanded] = React.useState<boolean>(true);
  const [domainDetailsOpen, setDomainDetailsOpen] = React.useState<Record<string, boolean>>({});

  React.useEffect(() => {
    if (preflightActive) {
      setPreflightExpanded(true);
      setDomainDetailsOpen({});
    } else {
      setPreflightExpanded(true);
      setDomainDetailsOpen({});
    }
  }, [preflightActive]);

  const selectedCounts = React.useMemo(() => {
    if (!preflightActive || !preflight) return { domains: 0, anchors: 0 };
    const domainsDraft = Array.isArray(preflight.domainsDraft) ? preflight.domainsDraft : [];
    const anchorsDraft = Array.isArray(preflight.anchorsDraft) ? preflight.anchorsDraft : [];

    let domains = 0;
    for (const d of domainsDraft) {
      const cp = String(d?.canonicalPath || '').trim();
      if (!cp) continue;
      if (preflight.selectedDomains?.[cp] === false) continue;
      domains += 1;
    }
    let anchors = 0;
    for (const a of anchorsDraft) {
      const cp = String(a?.canonicalPath || '').trim();
      if (!cp) continue;
      if (preflight.selectedAnchors?.[cp] === false) continue;
      anchors += 1;
    }
    return { domains, anchors };
  }, [preflightActive, preflight]);

  const selectedAnchorCountryCodes = React.useMemo(() => {
    if (!preflightActive || !preflight) return [];
    const anchorsDraft = Array.isArray(preflight.anchorsDraft) ? preflight.anchorsDraft : [];
    const out: string[] = [];
    for (const a of anchorsDraft) {
      const cp = String(a?.canonicalPath || '').trim();
      if (!cp) continue;
      if (preflight.selectedAnchors?.[cp] === false) continue;
      if (!cp.startsWith('/loc/')) continue;
      const parts = cp.split('/').filter(Boolean);
      const iso2 = String(parts[1] || '').trim().toLowerCase();
      if (iso2.length === 2 && /^[a-z]{2}$/.test(iso2)) out.push(iso2);
    }
    return Array.from(new Set(out)).slice(0, 10);
  }, [preflightActive, preflight]);

  const parseDomainGeoTokens = (cp: string): string[] => {
    const s = String(cp || '').trim();
    if (!s.startsWith('/domain/v1/')) return [];
    const parts = s.split('/').filter(Boolean);
    // parts: ["domain","v1","{contractType}", k1, v1, k2, v2...]
    const kv = parts.slice(3);
    const geos: string[] = [];
    for (let i = 0; i + 1 < kv.length; i += 2) {
      const k = String(kv[i] || '').trim().toLowerCase();
      const v = String(kv[i + 1] || '').trim().toLowerCase();
      if (k === 'geo' && v) {
        // Support composite geo tokens (ae-sa) as well as repeated geo pairs.
        if (v.includes('-')) {
          for (const p of v.split('-')) {
            const cc = String(p || '').trim().toLowerCase();
            if (cc.length === 2 && /^[a-z]{2}$/.test(cc)) geos.push(cc);
          }
        } else {
          geos.push(v);
        }
      }
    }
    return geos;
  };

  const buildBilateralDomain = (ccA: string, ccB: string, aName?: string, bName?: string) => {
    const a = String(ccA || '').toLowerCase();
    const b = String(ccB || '').toLowerCase();
    const [x, y] = [a, b].sort();
    const canonicalPath = `/domain/v1/bilateral-relations/geo/${x}-${y}`;
    const left = String(aName || '').trim();
    const right = String(bName || '').trim();
    const title =
      left && right ? `Bilateral relations: ${left} ↔ ${right}` : `Bilateral relations: ${x.toUpperCase()} ↔ ${y.toUpperCase()}`;
    const description = 'Research bilateral relations and first-order entities implied by this relationship.';
    return { canonicalPath, title, description };
  };

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
      if (!isCreatingLens && !preflightActive) return [];
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
  }, [events, lensId, projectId, isCreatingLens, preflightActive, activeCreateCorrelationId, activeCreateSessionId]);

  const updatePinnedState = React.useCallback(() => {
    const el = feedRef.current;
    if (!el) return;
    // If the user is within N px of the bottom, treat it as "pinned".
    const thresholdPx = 64;
    const remaining = el.scrollHeight - el.scrollTop - el.clientHeight;
    const nextPinned = remaining <= thresholdPx;
    if (nextPinned !== pinnedToBottomRef.current) {
      pinnedToBottomRef.current = nextPinned;
      setPinnedToBottom(nextPinned);
    }
  }, []);

  // Auto-scroll to bottom when new events arrive, but only if user is pinned to the bottom.
  React.useEffect(() => {
    const el = feedRef.current;
    if (!el) return;
    if (!pinnedToBottomRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [filteredEvents.length]);

  // When switching lens / creation session, pin to bottom by default.
  React.useEffect(() => {
    pinnedToBottomRef.current = true;
    setPinnedToBottom(true);
    const el = feedRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lensId, projectId, isCreatingLens, activeCreateCorrelationId, activeCreateSessionId]);

  const handleAssemble = React.useCallback((request: AssembleLensRequest) => {
    if (onAssemble) {
      onAssemble(request);
    }
  }, [onAssemble]);

  const hasEvents = filteredEvents.length > 0;
  // Shift input to bottom if we have events or creation is in progress
  const isContentActive = hasEvents || isCreatingLens || preflightActive;
  const handleSaveLog = React.useCallback(() => {
    if (!filteredEvents.length) return;
    const lines = filteredEvents.map((ev) => {
      const ts = new Date(ev.ts).toISOString();
      const title = ev.humanReadable?.title || ev.text || '';
      const description = ev.humanReadable?.description || '';
      const payload = description ? `${title} — ${description}` : title;
      return `${ts}\t${ev.type}\t${payload}`.trim();
    });
    const content = lines.join('\n') + '\n';
    const slug = lensId || projectId || 'session';
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `horizon-log-${slug}-${stamp}.txt`;
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  }, [filteredEvents, lensId, projectId]);

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
          onScroll={updatePinnedState}
          style={{
            flex: 1,
            minHeight: 0,
            width: '100%',
            overflowY: 'auto',
            padding: '16px 24px', // Match input container padding
          }}
        >
          {/* Constrain width to match input field below */}
          <div style={{ maxWidth: 960, margin: '0 auto', position: 'relative', paddingLeft: 36 }}>
            {preflightActive && preflight && (
              <div
                style={{
                  marginBottom: 14,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-elevated)',
                  borderRadius: 14,
                  padding: 16,
                  boxShadow: '0 12px 28px rgba(0,0,0,0.12)',
                }}
              >
                <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                  <div>
                    <div style={{ fontSize: 14, fontWeight: 800, color: 'var(--text-primary)' }}>
                      {preflightExpanded ? 'Confirm intent' : 'Intent confirmed'}
                    </div>
                    <div style={{ marginTop: 4, fontSize: 12, color: 'var(--text-tertiary)', lineHeight: 1.35 }}>
                      {preflightExpanded
                        ? (String(preflight.reason || '').trim() || 'Review and adjust what we will research before starting.')
                        : `${selectedCounts.domains} domain${selectedCounts.domains === 1 ? '' : 's'}, ${selectedCounts.anchors} anchor${selectedCounts.anchors === 1 ? '' : 's'}`}
                    </div>
                  </div>
                  <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
                    {!preflightExpanded && (
                      <button
                        type="button"
                        onClick={() => setPreflightExpanded(true)}
                        disabled={preflight.isBusy}
                        style={{
                          border: '1px solid var(--border-subtle)',
                          background: 'transparent',
                          color: 'var(--text-secondary-strong)',
                          borderRadius: 10,
                          padding: '8px 10px',
                          fontSize: 12,
                          fontWeight: 700,
                          cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                          opacity: preflight.isBusy ? 0.6 : 1,
                        }}
                      >
                        Edit
                      </button>
                    )}
                  </div>
                </div>

                {preflightExpanded && (
                  <div style={{ marginTop: 14, display: 'grid', gap: 12 }}>

                  <div
                    style={{
                      border: '1px solid var(--border-subtle)',
                      borderRadius: 12,
                      padding: 12,
                      background: 'var(--bg-subtle)',
                    }}
                  >
                    <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-secondary-strong)' }}>
                      Focus
                    </div>
                    <input
                      value={preflight.focus}
                      onChange={(e) => preflight.setFocus(e.target.value)}
                      disabled={preflight.isBusy}
                      placeholder="Short reusable intent statement"
                      style={{
                        marginTop: 8,
                        width: '100%',
                        borderRadius: 10,
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-primary)',
                        padding: '10px 12px',
                        fontSize: 13,
                        outline: 'none',
                      }}
                    />
                  </div>

                  <div style={{ display: 'grid', gap: 12, gridTemplateColumns: '1fr', alignItems: 'start' }}>
                    <div
                      style={{
                        border: '1px solid var(--border-subtle)',
                        borderRadius: 12,
                        padding: 12,
                        background: 'var(--bg-subtle)',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                        <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>
                          Domains
                        </div>
                        <button
                          type="button"
                          disabled={preflight.isBusy}
                          onClick={() => preflight.setDomainsDraft([...(preflight.domainsDraft || []), { canonicalPath: '', title: '', description: '' }])}
                          style={{
                            border: '1px solid var(--border-subtle)',
                            background: 'transparent',
                            color: 'var(--text-tertiary)',
                            borderRadius: 10,
                            padding: '6px 10px',
                            fontSize: 12,
                            fontWeight: 650,
                            cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                            opacity: preflight.isBusy ? 0.6 : 1,
                          }}
                        >
                          + Add domain
                        </button>
                      </div>
                      {selectedAnchorCountryCodes.length === 2 && (
                        <div style={{ marginTop: 10, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                          <button
                            type="button"
                            disabled={preflight.isBusy}
                            onClick={() => {
                              const [cc1, cc2] = selectedAnchorCountryCodes;
                              const anchorsDraft = Array.isArray(preflight.anchorsDraft) ? preflight.anchorsDraft : [];
                              const nameByCc: Record<string, string> = {};
                              for (const a of anchorsDraft) {
                                const cp = String(a?.canonicalPath || '').trim();
                                if (!cp.startsWith('/loc/')) continue;
                                const parts = cp.split('/').filter(Boolean);
                                const iso2 = String(parts[1] || '').trim().toLowerCase();
                                if (iso2 && String(a?.name || '').trim()) nameByCc[iso2] = String(a?.name || '').trim();
                              }
                              const built = buildBilateralDomain(cc1, cc2, nameByCc[cc1], nameByCc[cc2]);

                              const next = [...(preflight.domainsDraft || [])];
                              next.unshift(built);
                              preflight.setDomainsDraft(next);
                              preflight.setSelectedDomains((prev) => ({ ...(prev || {}), [built.canonicalPath]: true }));
                            }}
                            style={{
                              border: '1px solid rgba(59,130,246,0.35)',
                              background: 'rgba(59,130,246,0.10)',
                              color: 'var(--text-secondary-strong)',
                              borderRadius: 999,
                              padding: '6px 10px',
                              fontSize: 12,
                              fontWeight: 700,
                              cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                              opacity: preflight.isBusy ? 0.6 : 1,
                            }}
                          >
                            Create bilateral-relations domain from anchors
                          </button>
                          {(() => {
                            const domainsDraft = Array.isArray(preflight.domainsDraft) ? preflight.domainsDraft : [];
                            const idx = domainsDraft.findIndex((d: any) =>
                              String(d?.canonicalPath || '').trim().startsWith('/domain/v1/bilateral-relations/'),
                            );
                            if (idx < 0) return null;
                            const cp = String(domainsDraft[idx]?.canonicalPath || '').trim();
                            const geos = parseDomainGeoTokens(cp);
                            const want = [...selectedAnchorCountryCodes].map((x) => x.toLowerCase()).sort().join(',');
                            const have = [...geos].map((x) => x.toLowerCase()).sort().join(',');
                            if (!want || want === have) return null;

                            return (
                              <button
                                type="button"
                                disabled={preflight.isBusy}
                                onClick={() => {
                                  const [cc1, cc2] = selectedAnchorCountryCodes;
                                  const anchorsDraft = Array.isArray(preflight.anchorsDraft) ? preflight.anchorsDraft : [];
                                  const nameByCc: Record<string, string> = {};
                                  for (const a of anchorsDraft) {
                                    const acp = String(a?.canonicalPath || '').trim();
                                    if (!acp.startsWith('/loc/')) continue;
                                    const parts = acp.split('/').filter(Boolean);
                                    const iso2 = String(parts[1] || '').trim().toLowerCase();
                                    if (iso2 && String(a?.name || '').trim()) nameByCc[iso2] = String(a?.name || '').trim();
                                  }
                                  const built = buildBilateralDomain(cc1, cc2, nameByCc[cc1], nameByCc[cc2]);

                                  const next = [...(preflight.domainsDraft || [])];
                                  const prevCp = String(next[idx]?.canonicalPath || '').trim();
                                  next[idx] = { ...(next[idx] || {}), ...built };
                                  preflight.setDomainsDraft(next);
                                  preflight.setSelectedDomains((prev) => {
                                    const p = { ...(prev || {}) };
                                    if (prevCp && prevCp !== built.canonicalPath) delete p[prevCp];
                                    p[built.canonicalPath] = true;
                                    return p;
                                  });
                                }}
                                style={{
                                  border: '1px solid rgba(234,179,8,0.45)',
                                  background: 'rgba(234,179,8,0.10)',
                                  color: 'var(--text-secondary-strong)',
                                  borderRadius: 999,
                                  padding: '6px 10px',
                                  fontSize: 12,
                                  fontWeight: 700,
                                  cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                                  opacity: preflight.isBusy ? 0.6 : 1,
                                }}
                              >
                                Fix domain geo to match anchors
                              </button>
                            );
                          })()}
                        </div>
                      )}
                      <div style={{ marginTop: 10, display: 'grid', gap: 10 }}>
                        {(preflight.domainsDraft || []).length === 0 ? (
                          <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>No domains detected.</div>
                        ) : (
                          (preflight.domainsDraft || []).map((d: any, idx: number) => {
                            const cp = String(d?.canonicalPath || '');
                            const ok = !cp.trim() || preflight.isValidCanonicalPath(cp);
                            const checked = preflight.selectedDomains[String(cp || '').trim()] !== false;
                            const detailKey = String(cp || '').trim() || `d-${idx}`;
                            const detailsOpen = !!domainDetailsOpen[detailKey];
                            return (
                              <div key={`d-${idx}`} style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10, background: 'var(--bg-elevated)' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={(e) => {
                                      const key = String(cp || '').trim();
                                      if (!key) return;
                                      preflight.setSelectedDomains((prev) => ({ ...(prev || {}), [key]: e.target.checked }));
                                    }}
                                    disabled={preflight.isBusy || !String(cp || '').trim()}
                                  />
                                  <input
                                    value={cp}
                                    onChange={(e) => {
                                      const next = [...(preflight.domainsDraft || [])];
                                      next[idx] = { ...(next[idx] || {}), canonicalPath: e.target.value };
                                      preflight.setDomainsDraft(next);
                                    }}
                                    disabled={preflight.isBusy}
                                    placeholder="/domain/v1/..."
                                    style={{
                                      flex: 1,
                                      borderRadius: 10,
                                      border: `1px solid ${ok ? 'var(--border-subtle)' : 'rgba(239,68,68,0.5)'}`,
                                      background: 'transparent',
                                      color: 'var(--text-primary)',
                                      padding: '8px 10px',
                                      fontSize: 12,
                                      fontFamily: 'monospace',
                                      outline: 'none',
                                    }}
                                  />
                                </div>
                                {!detailsOpen ? (
                                  <div style={{ marginTop: 8 }}>
                                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'baseline' }}>
                                      <div style={{ fontSize: 12, fontWeight: 750, color: 'var(--text-primary)' }}>
                                        {String(d?.title || '').trim() || 'Untitled domain'}
                                      </div>
                                      <button
                                        type="button"
                                        disabled={preflight.isBusy}
                                        onClick={() =>
                                          setDomainDetailsOpen((prev) => ({ ...(prev || {}), [detailKey]: true }))
                                        }
                                        style={{
                                          border: '1px solid var(--border-subtle)',
                                          background: 'transparent',
                                          color: 'var(--text-tertiary)',
                                          borderRadius: 10,
                                          padding: '6px 10px',
                                          fontSize: 12,
                                          fontWeight: 650,
                                          cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                                          opacity: preflight.isBusy ? 0.6 : 1,
                                        }}
                                      >
                                        Edit details
                                      </button>
                                    </div>
                                    {!!String(d?.description || '').trim() && (
                                      <div
                                        style={{
                                          marginTop: 4,
                                          fontSize: 12,
                                          color: 'var(--text-tertiary)',
                                          display: '-webkit-box',
                                          WebkitLineClamp: 2,
                                          WebkitBoxOrient: 'vertical' as any,
                                          overflow: 'hidden',
                                        }}
                                      >
                                        {String(d?.description || '').trim()}
                                      </div>
                                    )}
                                  </div>
                                ) : (
                                  <div style={{ marginTop: 8, display: 'grid', gap: 8 }}>
                                    <input
                                      value={String(d?.title || '')}
                                      onChange={(e) => {
                                        const next = [...(preflight.domainsDraft || [])];
                                        next[idx] = { ...(next[idx] || {}), title: e.target.value };
                                        preflight.setDomainsDraft(next);
                                      }}
                                      disabled={preflight.isBusy}
                                      placeholder="Title"
                                      style={{
                                        width: '100%',
                                        borderRadius: 10,
                                        border: '1px solid var(--border-subtle)',
                                        background: 'transparent',
                                        color: 'var(--text-primary)',
                                        padding: '8px 10px',
                                        fontSize: 12,
                                        outline: 'none',
                                      }}
                                    />
                                    <input
                                      value={String(d?.description || '')}
                                      onChange={(e) => {
                                        const next = [...(preflight.domainsDraft || [])];
                                        next[idx] = { ...(next[idx] || {}), description: e.target.value };
                                        preflight.setDomainsDraft(next);
                                      }}
                                      disabled={preflight.isBusy}
                                      placeholder="Description"
                                      style={{
                                        width: '100%',
                                        borderRadius: 10,
                                        border: '1px solid var(--border-subtle)',
                                        background: 'transparent',
                                        color: 'var(--text-primary)',
                                        padding: '8px 10px',
                                        fontSize: 12,
                                        outline: 'none',
                                      }}
                                    />
                                    <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                                      <button
                                        type="button"
                                        disabled={preflight.isBusy}
                                        onClick={() =>
                                          setDomainDetailsOpen((prev) => ({ ...(prev || {}), [detailKey]: false }))
                                        }
                                        style={{
                                          border: '1px solid var(--border-subtle)',
                                          background: 'transparent',
                                          color: 'var(--text-tertiary)',
                                          borderRadius: 10,
                                          padding: '6px 10px',
                                          fontSize: 12,
                                          fontWeight: 650,
                                          cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                                          opacity: preflight.isBusy ? 0.6 : 1,
                                        }}
                                      >
                                        Collapse
                                      </button>
                                    </div>
                                  </div>
                                )}
                              </div>
                            );
                          })
                        )}
                      </div>
                    </div>

                    <div
                      style={{
                        border: '1px solid var(--border-subtle)',
                        borderRadius: 12,
                        padding: 12,
                        background: 'var(--bg-subtle)',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                        <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-secondary-strong)' }}>
                          Anchors
                        </div>
                        <button
                          type="button"
                          disabled={preflight.isBusy}
                          onClick={() => preflight.setAnchorsDraft([...(preflight.anchorsDraft || []), { canonicalPath: '', name: '' }])}
                          style={{
                            border: '1px solid var(--border-subtle)',
                            background: 'transparent',
                            color: 'var(--text-tertiary)',
                            borderRadius: 10,
                            padding: '6px 10px',
                            fontSize: 12,
                            fontWeight: 650,
                            cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                            opacity: preflight.isBusy ? 0.6 : 1,
                          }}
                        >
                          + Add anchor
                        </button>
                      </div>
                      <div style={{ marginTop: 10, display: 'grid', gap: 10 }}>
                        {(preflight.anchorsDraft || []).length === 0 ? (
                          <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>No anchors detected.</div>
                        ) : (
                          (preflight.anchorsDraft || []).map((a: any, idx: number) => {
                            const cp = String(a?.canonicalPath || '');
                            const ok = !cp.trim() || preflight.isValidCanonicalPath(cp);
                            const checked = preflight.selectedAnchors[String(cp || '').trim()] !== false;
                            return (
                              <div key={`a-${idx}`} style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10, background: 'var(--bg-elevated)' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={(e) => {
                                      const key = String(cp || '').trim();
                                      if (!key) return;
                                      preflight.setSelectedAnchors((prev) => ({ ...(prev || {}), [key]: e.target.checked }));
                                    }}
                                    disabled={preflight.isBusy || !String(cp || '').trim()}
                                  />
                                  <input
                                    value={String(a?.name || '')}
                                    onChange={(e) => {
                                      const next = [...(preflight.anchorsDraft || [])];
                                      next[idx] = { ...(next[idx] || {}), name: e.target.value };
                                      preflight.setAnchorsDraft(next);
                                    }}
                                    disabled={preflight.isBusy}
                                    placeholder="Anchor name"
                                    style={{
                                      flex: 1,
                                      borderRadius: 10,
                                      border: '1px solid var(--border-subtle)',
                                      background: 'transparent',
                                      color: 'var(--text-primary)',
                                      padding: '9px 10px',
                                      fontSize: 13,
                                      fontWeight: 750,
                                      outline: 'none',
                                    }}
                                  />
                                </div>
                                <div style={{ marginTop: 8, display: 'flex', alignItems: 'center', gap: 10 }}>
                                  <input
                                    value={cp}
                                    onChange={(e) => {
                                      const next = [...(preflight.anchorsDraft || [])];
                                      next[idx] = { ...(next[idx] || {}), canonicalPath: e.target.value };
                                      preflight.setAnchorsDraft(next);
                                    }}
                                    disabled={preflight.isBusy}
                                    placeholder="/loc/... /org/... /gov/... /domain/..."
                                    style={{
                                      flex: 1,
                                      borderRadius: 10,
                                      border: `1px solid ${ok ? 'var(--border-subtle)' : 'rgba(239,68,68,0.5)'}`,
                                      background: 'rgba(255,255,255,0.02)',
                                      color: 'var(--text-primary)',
                                      padding: '7px 10px',
                                      fontSize: 12,
                                      fontFamily: 'monospace',
                                      outline: 'none',
                                    }}
                                  />
                                </div>
                              </div>
                            );
                          })
                        )}
                      </div>
                    </div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                    <button
                      type="button"
                      onClick={preflight.onCancel}
                      disabled={preflight.isBusy}
                      style={{
                        border: '1px solid var(--border-subtle)',
                        background: 'transparent',
                        color: 'var(--text-secondary-strong)',
                        borderRadius: 10,
                        padding: '9px 12px',
                        fontSize: 12,
                        fontWeight: 700,
                        cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                        opacity: preflight.isBusy ? 0.6 : 1,
                      }}
                    >
                      Cancel
                    </button>
                    <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                      <button
                        type="button"
                        onClick={() => {
                          setPreflightExpanded(false);
                          preflight.setMode('just_open');
                          preflight.onContinue('just_open');
                        }}
                        disabled={preflight.isBusy}
                        style={{
                          border: '1px solid rgba(59,130,246,0.35)',
                          background: 'rgba(59,130,246,0.10)',
                          color: 'var(--text-primary)',
                          borderRadius: 10,
                          padding: '9px 12px',
                          fontSize: 12,
                          fontWeight: 750,
                          cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                          opacity: preflight.isBusy ? 0.6 : 1,
                        }}
                      >
                        Just open
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setPreflightExpanded(false);
                          preflight.setMode('research_first');
                          preflight.onContinue('research_first');
                        }}
                        disabled={preflight.isBusy}
                        style={{
                          border: '1px solid rgba(59,130,246,0.45)',
                          background: 'rgba(59,130,246,0.16)',
                          color: 'var(--text-primary)',
                          borderRadius: 10,
                          padding: '9px 12px',
                          fontSize: 12,
                          fontWeight: 800,
                          cursor: preflight.isBusy ? 'not-allowed' : 'pointer',
                          opacity: preflight.isBusy ? 0.6 : 1,
                        }}
                      >
                        Research
                      </button>
                    </div>
                  </div>
                </div>
                )}
              </div>
            )}

            <div style={{ position: 'sticky', top: 0, zIndex: 2, display: 'flex', justifyContent: 'flex-end', paddingBottom: 8 }}>
              {!pinnedToBottom && (
                <button
                  type="button"
                  onClick={() => {
                    pinnedToBottomRef.current = true;
                    setPinnedToBottom(true);
                    const el = feedRef.current;
                    if (!el) return;
                    el.scrollTop = el.scrollHeight;
                  }}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                    color: 'var(--text-secondary-strong)',
                    borderRadius: 6,
                    padding: '6px 10px',
                    fontSize: 12,
                    cursor: 'pointer',
                    marginRight: 8,
                  }}
                  title="Jump to latest"
                >
                  Jump to latest
                </button>
              )}
              <button
                type="button"
                onClick={handleSaveLog}
                disabled={!filteredEvents.length}
                style={{
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-secondary-strong)',
                  borderRadius: 6,
                  padding: '6px 10px',
                  fontSize: 12,
                  cursor: filteredEvents.length ? 'pointer' : 'not-allowed',
                }}
              >
                Save log
              </button>
            </div>
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
                    Preparing your analysis...
                  </div>
                  <div style={{ fontSize: 11, color: 'var(--text-tertiary)', fontFamily: 'monospace' }}>
                    Request submitted
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
                
                const s =
                  ev.data?.sourceCanonicalPath ||
                  ev.data?.qualifiers?.sourceCanonicalPath ||
                  ev.data?.sourceLabel ||
                  ev.data?.sourceFragmentId ||
                  '';
                const t =
                  ev.data?.targetCanonicalPath ||
                  ev.data?.qualifiers?.targetCanonicalPath ||
                  ev.data?.targetLabel ||
                  ev.data?.targetFragmentId ||
                  '';
                
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
            disabled={isCreatingLens || preflightActive}
            pinnedBottom={isContentActive}
          />
        </div>
      )}
    </div>
  );
};
