import React from 'react';
import type { HorizonRealtimeEvent } from '../stores/horizonRealtimeStore';

/* ------------------------------------------------------------------ */
/*  Types                                                             */
/* ------------------------------------------------------------------ */

export type DomainOverlayPanelMode = 'mini' | 'list' | 'expanded';
type Tone = 'info' | 'success' | 'warning' | 'error';

export interface DomainOverlayAnswerItem {
  key: string;
  raw: any;
  snapshotId: string;
  domainId: string;
  canonicalPath: string;
  contractType: string;
  title: string;
  readinessStatus: string;
  thesisText: string;
  dataGaps: any[];
  criticalDependencies: any[];
  explanatoryUnits: any[];
  impactPaths: any[];
  criticalNodes: any[];
  concentrationEvidence: any[];
  controlExposure: any[];
  highlightedNodeIds: string[];
  highlightedEdgeIds: string[];
  gateFailedCount: number;
  facets: Record<string, string[]>;
  assembledAt?: string;
}

export interface DomainOverlayPanelProps {
  theme: 'light' | 'dark';
  mode: DomainOverlayPanelMode;
  setMode: (m: DomainOverlayPanelMode) => void;
  items: DomainOverlayAnswerItem[];
  activeKey: string;
  setActiveKey: (k: string) => void;
  highlightEnabled: boolean;
  setHighlightEnabled: (v: boolean) => void;
  timeline: HorizonRealtimeEvent[];
  miniBadge: { label: string; tone: Tone } | null;
  domainContractEventTone: (ev: HorizonRealtimeEvent) => Tone;
  domainContractEventLabel: (ev: HorizonRealtimeEvent) => string;
  readinessMeta: (item?: DomainOverlayAnswerItem | null) => { label: string; tone: Tone };
  activeDomainOverlayPath: string;
  /** Always show mini button even if no data (e.g. when lens is loaded) */
  alwaysShowMini?: boolean;
  /** Create a new domain from a query */
  onCreateDomain?: (query: string) => void;
  /** Run DomainContractEngine for an existing domain */
  onRunDomain?: (item: DomainOverlayAnswerItem) => void;
  /** Per-dependency highlight callback */
  onHighlightDependency?: (dep: any) => void;
  onHoverDependency?: (dep: any | null) => void;
  onHoverExplanatoryUnit?: (unit: any | null) => void;
  onHighlightFacet?: (facetKey: string) => void;
  onClearHighlight?: () => void;
}

/* ------------------------------------------------------------------ */
/*  Helpers                                                           */
/* ------------------------------------------------------------------ */

const SECTION_MAX_HEIGHT = 190;

const toneColor = (tone: Tone, theme: string): string => {
  if (tone === 'error') return '#EF4444';
  if (tone === 'warning') return '#F59E0B';
  if (tone === 'success') return '#22C55E';
  return theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)';
};

const toneBg = (tone: Tone, theme: string): string => {
  const dark = theme === 'dark';
  if (tone === 'error') return dark ? 'rgba(239,68,68,0.14)' : 'rgba(239,68,68,0.10)';
  if (tone === 'warning') return dark ? 'rgba(245,158,11,0.14)' : 'rgba(245,158,11,0.10)';
  if (tone === 'success') return dark ? 'rgba(34,197,94,0.14)' : 'rgba(34,197,94,0.10)';
  return dark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)';
};

const toneBorder = (tone: Tone): string => {
  if (tone === 'error') return '1px solid rgba(239,68,68,0.30)';
  if (tone === 'warning') return '1px solid rgba(245,158,11,0.30)';
  if (tone === 'success') return '1px solid rgba(34,197,94,0.30)';
  return '1px solid var(--border-subtle)';
};

const pillStyle = (tone: Tone, theme: string): React.CSSProperties => ({
  display: 'inline-flex',
  alignItems: 'center',
  padding: '2px 8px',
  borderRadius: 999,
  fontSize: 10,
  fontWeight: 800,
  lineHeight: 1.3,
  border: toneBorder(tone),
  background: toneBg(tone, theme),
  color: toneColor(tone, theme),
});

const cardBg = (theme: string): string =>
  theme === 'dark' ? 'rgba(15,15,20,0.92)' : 'rgba(255,255,255,0.94)';

const cardShadow = (theme: string): string =>
  theme === 'dark'
    ? '0 12px 40px rgba(0,0,0,0.55)'
    : '0 12px 40px rgba(0,0,0,0.16)';

const OVERLAY_Z = 70;
const DOMAIN_LIST_WIDTH = 330; // 220 * 1.5
const DOMAIN_EXPANDED_WIDTH = 465; // 310 * 1.5
const nodeActionButtonStyle = (theme: string): React.CSSProperties => ({
  width: 24,
  height: 24,
  borderRadius: 9,
  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.25)' : 'rgba(15,23,42,0.18)'}`,
  background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.78)',
  color: theme === 'dark' ? 'rgba(255,255,255,0.88)' : 'rgba(15,23,42,0.78)',
  cursor: 'pointer',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  boxShadow: theme === 'dark' ? '0 8px 18px rgba(0,0,0,0.42)' : '0 8px 18px rgba(0,0,0,0.12)',
  flexShrink: 0,
});

/* ------------------------------------------------------------------ */
/*  Component                                                         */
/* ------------------------------------------------------------------ */

export const DomainOverlayPanel: React.FC<DomainOverlayPanelProps> = (props) => {
  const {
    theme, mode, setMode, items, activeKey, setActiveKey,
    highlightEnabled, setHighlightEnabled,
    timeline, miniBadge,
    domainContractEventTone, domainContractEventLabel,
    readinessMeta, activeDomainOverlayPath,
    alwaysShowMini = false,
    onCreateDomain, onRunDomain,
    onHighlightDependency, onHoverDependency, onHoverExplanatoryUnit, onHighlightFacet, onClearHighlight,
  } = props;

  const activeItem = React.useMemo(
    () => items.find((x) => x.key === activeKey) || items[0] || null,
    [items, activeKey],
  );

  // Create domain inline input
  const [showCreateInput, setShowCreateInput] = React.useState(false);
  const [createQuery, setCreateQuery] = React.useState('');
  const [createLoading, setCreateLoading] = React.useState(false);
  const createInputRef = React.useRef<HTMLInputElement | null>(null);

  React.useEffect(() => {
    if (showCreateInput) {
      const t = window.setTimeout(() => createInputRef.current?.focus(), 0);
      return () => window.clearTimeout(t);
    }
  }, [showCreateInput]);

  const handleCreateSubmit = React.useCallback(() => {
    const q = createQuery.trim();
    if (!q || createLoading || !onCreateDomain) return;
    setCreateLoading(true);
    try {
      onCreateDomain(q);
    } finally {
      setCreateQuery('');
      setShowCreateInput(false);
      setCreateLoading(false);
    }
  }, [createQuery, createLoading, onCreateDomain]);

  const closeDomainList = React.useCallback(() => {
    // Reset active domain selection so graph highlights are cleared.
    setActiveKey('');
    onClearHighlight?.();
    setMode('mini');
  }, [setActiveKey, onClearHighlight, setMode]);

  // Expanded accordion sections
  const [expandedSections, setExpandedSections] = React.useState<Set<string>>(new Set(['thesis']));
  const [hoveredDomainKey, setHoveredDomainKey] = React.useState<string>('');
  const [hoveredDependencyKey, setHoveredDependencyKey] = React.useState<string>('');
  const [hoveredExplanatoryKey, setHoveredExplanatoryKey] = React.useState<string>('');
  React.useEffect(() => {
    setHoveredDependencyKey('');
    setHoveredExplanatoryKey('');
    onHoverDependency?.(null);
    onHoverExplanatoryUnit?.(null);
  }, [activeItem?.key, mode, onHoverDependency, onHoverExplanatoryUnit]);
  const toggleSection = React.useCallback((s: string) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(s)) next.delete(s);
      else next.add(s);
      return next;
    });
  }, []);

  // Esc handling
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (mode === 'expanded') { e.preventDefault(); setMode('list'); return; }
      if (mode === 'list') { e.preventDefault(); closeDomainList(); return; }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [mode, setMode, closeDomainList]);

  if (items.length === 0 && !miniBadge && !alwaysShowMini) return null;

  /* ──────── State 1: Mini ──────── */
  if (mode === 'mini') {
    const count = items.length;
    return (
      <div style={{ position: 'relative', pointerEvents: 'auto' }}>
        {miniBadge && (
          <div
            style={{
              position: 'absolute',
              right: 0,
              bottom: 48,
              ...pillStyle(miniBadge.tone, theme),
              maxWidth: 180,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              boxShadow: theme === 'dark' ? '0 6px 16px rgba(0,0,0,0.4)' : '0 6px 16px rgba(0,0,0,0.12)',
              backdropFilter: 'blur(8px)',
            }}
            title={miniBadge.label}
          >
            {miniBadge.label}
          </div>
        )}
        <button
          type="button"
          onClick={() => setMode(items.length > 0 ? 'list' : 'mini')}
          title="Domain answers"
          style={{
            width: 40,
            height: 40,
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: cardBg(theme),
            color: 'var(--text-heading)',
            cursor: items.length > 0 ? 'pointer' : 'default',
            boxShadow: cardShadow(theme),
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 14,
            fontWeight: 900,
            backdropFilter: 'blur(8px)',
            position: 'relative',
          }}
        >
          D
          {count > 0 && (
            <span
              style={{
                position: 'absolute',
                top: -4,
                right: -4,
                minWidth: 16,
                height: 16,
                borderRadius: 999,
                background: 'var(--accent-primary, #3B82F6)',
                color: '#fff',
                fontSize: 9,
                fontWeight: 900,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                padding: '0 4px',
              }}
            >
              {count}
            </span>
          )}
        </button>
      </div>
    );
  }

  /* ──────── State 2: Domain List ──────── */
  if (mode === 'list') {
    return (
      <div
        style={{
          position: 'absolute',
          right: 0,
          bottom: 0,
          width: `min(92vw, ${DOMAIN_LIST_WIDTH}px)`,
          maxHeight: 'min(400px, 60vh)',
          borderRadius: 14,
          border: '1px solid var(--border-subtle)',
          background: cardBg(theme),
          boxShadow: cardShadow(theme),
          backdropFilter: 'blur(12px)',
          zIndex: OVERLAY_Z,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          pointerEvents: 'auto',
        }}
      >
        {/* Header */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '10px 12px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>
            Domains ({items.length})
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            {onCreateDomain && (
              <button
                type="button"
                onClick={() => setShowCreateInput((v) => !v)}
                style={{
                  border: 'none',
                  background: showCreateInput
                    ? (theme === 'dark' ? 'rgba(59,130,246,0.18)' : 'rgba(59,130,246,0.10)')
                    : 'transparent',
                  cursor: 'pointer',
                  fontSize: 14,
                  fontWeight: 900,
                  color: showCreateInput ? 'var(--accent-primary, #3B82F6)' : 'var(--text-secondary-strong)',
                  padding: '2px 6px',
                  borderRadius: 6,
                  lineHeight: 1,
                }}
                title="Create domain from query"
              >
                +
              </button>
            )}
            <button
              type="button"
              onClick={closeDomainList}
              style={nodeActionButtonStyle(theme)}
              title="Close domain list"
              aria-label="Close domain list"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M6 6l12 12M18 6l-12 12" />
              </svg>
            </button>
          </div>
        </div>

        {/* Create domain input */}
        {showCreateInput && (
          <div style={{ padding: '6px 8px', borderBottom: '1px solid var(--border-subtle)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <input
                ref={createInputRef}
                value={createQuery}
                onChange={(e) => setCreateQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') { e.preventDefault(); setShowCreateInput(false); setCreateQuery(''); return; }
                  if (e.key === 'Enter') { e.preventDefault(); handleCreateSubmit(); return; }
                }}
                placeholder="Ask a question..."
                autoComplete="off"
                spellCheck={false}
                disabled={createLoading}
                style={{
                  flex: 1,
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 8,
                  padding: '5px 8px',
                  fontSize: 11,
                  fontWeight: 500,
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  outline: 'none',
                }}
              />
              <button
                type="button"
                onClick={handleCreateSubmit}
                disabled={createLoading || !createQuery.trim()}
                style={{
                  border: 'none',
                  borderRadius: 8,
                  padding: '4px 8px',
                  fontSize: 10,
                  fontWeight: 800,
                  background: 'var(--accent-primary, #3B82F6)',
                  color: '#fff',
                  cursor: createLoading || !createQuery.trim() ? 'not-allowed' : 'pointer',
                  opacity: createLoading || !createQuery.trim() ? 0.5 : 1,
                }}
              >
                {createLoading ? '...' : 'Go'}
              </button>
            </div>
          </div>
        )}

        {/* List */}
        <div
          style={{
            flex: 1,
            minHeight: 0,
            overflowY: 'auto',
            padding: '6px 8px',
            display: 'flex',
            flexDirection: 'column',
            gap: 4,
            overscrollBehavior: 'contain',
          }}
        >
          {items.map((item) => {
            const meta = readinessMeta(item);
            const isActive = item.key === activeKey;
            const isHovered = item.key === hoveredDomainKey;
            const isPending = !item.snapshotId && item.readinessStatus === 'pending';
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => {
                  if (isPending && onRunDomain) {
                    onRunDomain(item);
                  }
                  setActiveKey(item.key);
                  setMode('expanded');
                }}
                onMouseEnter={() => setHoveredDomainKey(item.key)}
                onMouseLeave={() => setHoveredDomainKey((prev) => (prev === item.key ? '' : prev))}
                onFocus={() => setHoveredDomainKey(item.key)}
                onBlur={() => setHoveredDomainKey((prev) => (prev === item.key ? '' : prev))}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 6,
                  width: '100%',
                  padding: '8px 10px',
                  borderRadius: 10,
                  border: isActive
                    ? '1px solid var(--accent-primary, #3B82F6)'
                    : isHovered
                      ? (theme === 'dark' ? '1px solid rgba(59,130,246,0.44)' : '1px solid rgba(59,130,246,0.30)')
                      : '1px solid transparent',
                  background: isActive
                    ? (theme === 'dark' ? 'rgba(59,130,246,0.10)' : 'rgba(59,130,246,0.06)')
                    : isHovered
                      ? (theme === 'dark' ? 'rgba(59,130,246,0.08)' : 'rgba(59,130,246,0.05)')
                      : 'transparent',
                  boxShadow: isHovered && !isActive
                    ? (theme === 'dark' ? '0 6px 14px rgba(0,0,0,0.24)' : '0 6px 14px rgba(0,0,0,0.10)')
                    : 'none',
                  cursor: 'pointer',
                  textAlign: 'left',
                  transition: 'background 0.12s ease, border-color 0.12s ease, box-shadow 0.12s ease',
                }}
              >
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: 12,
                      fontWeight: 700,
                      color: 'var(--text-heading)',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                    title={item.title}
                  >
                    {item.title}
                  </div>
                  {item.contractType && (
                    <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginTop: 2 }}>
                      {item.contractType}
                    </div>
                  )}
                </div>
                <span style={pillStyle(meta.tone, theme)}>{meta.label}</span>
              </button>
            );
          })}
        </div>
      </div>
    );
  }

  /* ──────── State 3: Expanded ──────── */
  if (!activeItem) {
    setMode('list');
    return null;
  }
  const meta = readinessMeta(activeItem);

  return (
    <div
      style={{
        position: 'absolute',
        right: 0,
        bottom: 0,
        width: `min(92vw, ${DOMAIN_EXPANDED_WIDTH}px)`,
        maxHeight: 'min(70vh, 600px)',
        minHeight: 'min(38vh, 320px)',
        borderRadius: 14,
        border: '1px solid var(--border-subtle)',
        background: cardBg(theme),
        boxShadow: cardShadow(theme),
        backdropFilter: 'blur(12px)',
        zIndex: OVERLAY_Z,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        pointerEvents: 'auto',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '10px 12px',
          borderBottom: '1px solid var(--border-subtle)',
        }}
      >
        <button
          type="button"
          onClick={() => setMode('list')}
          title="Back to domain list"
          aria-label="Back to domain list"
          style={nodeActionButtonStyle(theme)}
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M15 6l-6 6 6 6" />
          </svg>
        </button>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 13,
              fontWeight: 800,
              color: 'var(--text-heading)',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
            title={activeItem.title}
          >
            {activeItem.title}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 3 }}>
            <span style={pillStyle(meta.tone, theme)}>{meta.label}</span>
            {activeItem.dataGaps.length > 0 && (
              <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                {activeItem.dataGaps.length} gap{activeItem.dataGaps.length !== 1 ? 's' : ''}
              </span>
            )}
          </div>
        </div>
        {/* Highlight toggle */}
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); setHighlightEnabled(!highlightEnabled); }}
          title={highlightEnabled ? 'Disable graph highlight' : 'Enable graph highlight'}
          style={{
            width: 28,
            height: 28,
            borderRadius: 8,
            border: '1px solid var(--border-subtle)',
            background: highlightEnabled
              ? (theme === 'dark' ? 'rgba(56,189,248,0.18)' : 'rgba(2,132,199,0.10)')
              : 'transparent',
            color: highlightEnabled ? '#38BDF8' : 'var(--text-secondary-strong)',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 13,
            fontWeight: 900,
          }}
        >
          ◎
        </button>
      </div>

      {/* Scrollable body */}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          overflowY: 'auto',
          padding: '8px 10px 16px',
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          overscrollBehavior: 'contain',
          scrollPaddingBottom: 16,
        }}
      >

        {/* ▸ Thesis */}
        <AccordionSection
          id="thesis"
          label="Thesis"
          expanded={expandedSections.has('thesis')}
          onToggle={toggleSection}
          theme={theme}
          maxBodyHeight={220}
        >
          {activeItem.thesisText ? (
            <div style={{ fontSize: 12, lineHeight: 1.55, color: 'var(--text-heading)' }}>
              {activeItem.thesisText}
            </div>
          ) : (
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontStyle: 'italic' }}>
              Thesis not yet assembled.
            </div>
          )}
          {activeItem.raw?.objectiveCheck?.completeness && (
            <div style={{ marginTop: 6, fontSize: 10, color: 'var(--text-secondary-strong)' }}>
              Confidence: {activeItem.raw.objectiveCheck.confidence != null
                ? `${Math.round(activeItem.raw.objectiveCheck.confidence * 100)}%`
                : 'N/A'}
            </div>
          )}
        </AccordionSection>

        {/* ▸ Top Dependencies */}
        <AccordionSection
          id="deps"
          label={`Top Dependencies (${activeItem.criticalDependencies.length})`}
          expanded={expandedSections.has('deps')}
          onToggle={toggleSection}
          theme={theme}
          maxBodyHeight={220}
        >
          {activeItem.criticalDependencies.length === 0 ? (
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontStyle: 'italic' }}>None found.</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              {activeItem.criticalDependencies.slice(0, 10).map((dep: any, idx: number) => {
                const src = String(dep?.sourceCanonicalPath || '').split('/').pop() || '?';
                const tgt = String(dep?.targetCanonicalPath || '').split('/').pop() || '?';
                const score = Number(dep?.topDependencyScore || 0);
                const facet = String(dep?.facetKey || '').trim();
                const rowKey = String(dep?.edgeId || idx);
                const isHovered = hoveredDependencyKey === rowKey;
                return (
                  <div
                    key={rowKey}
                    style={{
                      padding: '6px 8px',
                      borderRadius: 8,
                      border: isHovered
                        ? (theme === 'dark' ? '1px solid rgba(59,130,246,0.44)' : '1px solid rgba(59,130,246,0.30)')
                        : '1px solid var(--border-subtle)',
                      background: isHovered
                        ? (theme === 'dark' ? 'rgba(59,130,246,0.08)' : 'rgba(59,130,246,0.05)')
                        : (theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)'),
                      cursor: onHoverDependency || onHighlightDependency ? 'pointer' : 'default',
                      transition: 'background 0.12s ease, border-color 0.12s ease',
                    }}
                    onClick={() => onHighlightDependency?.(dep)}
                    onMouseEnter={() => {
                      setHoveredDependencyKey(rowKey);
                      onHoverDependency?.(dep);
                    }}
                    onMouseLeave={() => {
                      setHoveredDependencyKey((prev) => (prev === rowKey ? '' : prev));
                      onHoverDependency?.(null);
                    }}
                    onFocus={() => {
                      setHoveredDependencyKey(rowKey);
                      onHoverDependency?.(dep);
                    }}
                    onBlur={() => {
                      setHoveredDependencyKey((prev) => (prev === rowKey ? '' : prev));
                      onHoverDependency?.(null);
                    }}
                    title={`${dep?.sourceCanonicalPath} → ${dep?.targetCanonicalPath}`}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ fontSize: 10, fontWeight: 800, color: 'var(--text-secondary-strong)', minWidth: 14 }}>
                        {idx + 1}
                      </span>
                      <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-heading)', flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {src} → {tgt}
                      </span>
                      <ScoreBar score={score} theme={theme} />
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 3 }}>
                      <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                        {String(dep?.relationType || '').replace(/_/g, ' ')}
                      </span>
                      {facet && facet !== 'default' && (
                        <span style={{
                          ...pillStyle('info', theme),
                          fontSize: 9,
                          padding: '1px 5px',
                          cursor: onHighlightFacet ? 'pointer' : 'default',
                        }}
                          onClick={(e) => { e.stopPropagation(); onHighlightFacet?.(facet); }}
                        >
                          {facet}
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </AccordionSection>

        {/* ▸ Mechanisms */}
        {activeItem.explanatoryUnits.length > 0 && (
          <AccordionSection
            id="mechanisms"
            label={`Mechanisms (${activeItem.explanatoryUnits.length})`}
            expanded={expandedSections.has('mechanisms')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={260}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              {activeItem.explanatoryUnits.slice(0, 80).map((unit: any, idx: number) => {
                const src = String(unit?.sourceCanonicalPath || '').split('/').pop() || '?';
                const tgt = String(unit?.targetCanonicalPath || '').split('/').pop() || '?';
                const rank = Number(unit?.dependencyRank || 0) || idx + 1;
                const mechanism = String(unit?.mechanism || '').trim();
                const role = String(unit?.roleInThesis || '').trim();
                const explanation = String(unit?.explanationText || '').trim();
                const confidence = Number(unit?.confidence || 0);
                const rowKey = String(unit?.unitId || unit?.dependencyEdgeId || idx);
                const isHovered = hoveredExplanatoryKey === rowKey;
                return (
                  <div
                    key={rowKey}
                    style={{
                      padding: '6px 8px',
                      borderRadius: 8,
                      border: isHovered
                        ? (theme === 'dark' ? '1px solid rgba(56,189,248,0.48)' : '1px solid rgba(2,132,199,0.34)')
                        : '1px solid var(--border-subtle)',
                      background: isHovered
                        ? (theme === 'dark' ? 'rgba(56,189,248,0.10)' : 'rgba(2,132,199,0.06)')
                        : (theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)'),
                      cursor: onHoverExplanatoryUnit ? 'pointer' : 'default',
                      transition: 'background 0.12s ease, border-color 0.12s ease',
                    }}
                    onMouseEnter={() => {
                      setHoveredExplanatoryKey(rowKey);
                      onHoverExplanatoryUnit?.(unit);
                    }}
                    onMouseLeave={() => {
                      setHoveredExplanatoryKey((prev) => (prev === rowKey ? '' : prev));
                      onHoverExplanatoryUnit?.(null);
                    }}
                    onFocus={() => {
                      setHoveredExplanatoryKey(rowKey);
                      onHoverExplanatoryUnit?.(unit);
                    }}
                    onBlur={() => {
                      setHoveredExplanatoryKey((prev) => (prev === rowKey ? '' : prev));
                      onHoverExplanatoryUnit?.(null);
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{ fontSize: 10, fontWeight: 800, color: 'var(--text-secondary-strong)', minWidth: 14 }}>
                        {rank}
                      </span>
                      <span
                        style={{
                          fontSize: 11,
                          fontWeight: 700,
                          color: 'var(--text-heading)',
                          flex: 1,
                          minWidth: 0,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {src} → {tgt}
                      </span>
                      {confidence > 0 && <ScoreBar score={confidence} theme={theme} />}
                    </div>
                    {explanation && (
                      <div style={{ marginTop: 4, fontSize: 10, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        {explanation}
                      </div>
                    )}
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 4, flexWrap: 'wrap' }}>
                      {mechanism && (
                        <span style={{ ...pillStyle('info', theme), fontSize: 9, padding: '1px 6px' }}>
                          {mechanism}
                        </span>
                      )}
                      {role && (
                        <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                          {role}
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </AccordionSection>
        )}

        {/* ▸ Concentration Hotspots */}
        {activeItem.concentrationEvidence.length > 0 && (
          <AccordionSection
            id="concentration"
            label={`Concentration (${activeItem.concentrationEvidence.length})`}
            expanded={expandedSections.has('concentration')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={220}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              {activeItem.concentrationEvidence.slice(0, 8).map((ev: any, idx: number) => {
                const path = String(ev?.marketCanonicalPath || '').split('/').pop() || 'Market';
                const metrics: string[] = [];
                if (ev?.cr4 != null) metrics.push(`CR4 ${(Number(ev.cr4) * 100).toFixed(0)}%`);
                if (ev?.hhi != null) metrics.push(`HHI ${Number(ev.hhi).toFixed(0)}`);
                if (ev?.tam != null) metrics.push(`TAM $${Number(ev.tam).toFixed(0)}`);
                if (ev?.cagr != null) metrics.push(`CAGR ${(Number(ev.cagr) * 100).toFixed(1)}%`);
                if (ev?.dataGap) metrics.push(`⚠ ${ev.dataGap}`);
                return (
                  <div key={idx} style={{ fontSize: 11, color: 'var(--text-heading)', padding: '3px 0' }}>
                    <span style={{ fontWeight: 700 }}>{path}</span>
                    {metrics.length > 0 && (
                      <span style={{ color: 'var(--text-secondary-strong)', marginLeft: 6 }}>
                        {metrics.join(' · ')}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </AccordionSection>
        )}

        {/* ▸ Regulatory / Control */}
        {activeItem.controlExposure.length > 0 && (
          <AccordionSection
            id="control"
            label={`Regulatory (${activeItem.controlExposure.length})`}
            expanded={expandedSections.has('control')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={220}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              {activeItem.controlExposure.slice(0, 8).map((ce: any, idx: number) => {
                const sev = String(ce?.severity || 'medium').trim();
                const sevTone: Tone = sev === 'high' ? 'error' : sev === 'medium' ? 'warning' : 'info';
                return (
                  <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11 }}>
                    <span style={pillStyle(sevTone, theme)}>{sev}</span>
                    <span style={{ color: 'var(--text-heading)' }}>
                      {String(ce?.mechanismType || '').replace(/_/g, ' ')}
                    </span>
                    {Array.isArray(ce?.path) && ce.path.length > 0 && (
                      <span style={{ color: 'var(--text-secondary-strong)', fontSize: 10 }}>
                        {ce.path.map((p: string) => String(p).split('/').pop()).join(' → ')}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </AccordionSection>
        )}

        {/* ▸ Data Gaps */}
        {activeItem.dataGaps.length > 0 && (
          <AccordionSection
            id="gaps"
            label={`Data Gaps (${activeItem.dataGaps.length})`}
            expanded={expandedSections.has('gaps')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={220}
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              {activeItem.dataGaps.slice(0, 10).map((gap: any, idx: number) => (
                <div key={idx} style={{ fontSize: 11, padding: '3px 0' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span style={pillStyle(String(gap?.priority || '') === 'P0' ? 'error' : 'warning', theme)}>
                      {String(gap?.priority || 'P2')}
                    </span>
                    <span style={{ color: 'var(--text-heading)', fontWeight: 600 }}>
                      {String(gap?.gapType || '').replace(/_/g, ' ')}
                    </span>
                  </div>
                  {gap?.hint && (
                    <div style={{ color: 'var(--text-secondary-strong)', fontSize: 10, marginTop: 2, paddingLeft: 2 }}>
                      {String(gap.hint)}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </AccordionSection>
        )}

        {/* ▸ Facets */}
        {Object.keys(activeItem.facets).length > 0 && (
          <AccordionSection
            id="facets"
            label="Facets"
            expanded={expandedSections.has('facets')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={180}
            bodyPadding="4px 10px 12px"
          >
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {Object.entries(activeItem.facets).map(([key, vals]) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => onHighlightFacet?.(key)}
                  style={{
                    ...pillStyle('info', theme),
                    cursor: onHighlightFacet ? 'pointer' : 'default',
                    fontSize: 10,
                    padding: '3px 8px',
                  }}
                  title={`${key}: ${Array.isArray(vals) ? vals.join(', ') : ''}`}
                >
                  {key} {Array.isArray(vals) && vals.length > 0 ? `(${vals.length})` : ''}
                </button>
              ))}
            </div>
          </AccordionSection>
        )}

        {/* ▸ Timeline */}
        {timeline.length > 0 && (
          <AccordionSection
            id="timeline"
            label="Timeline"
            expanded={expandedSections.has('timeline')}
            onToggle={toggleSection}
            theme={theme}
            maxBodyHeight={240}
          >
            {activeDomainOverlayPath && (
              <div
                style={{
                  marginBottom: 6,
                  fontSize: 10,
                  color: 'var(--text-secondary-strong)',
                  fontFamily: 'monospace',
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
                title={activeDomainOverlayPath}
              >
                {activeDomainOverlayPath}
              </div>
            )}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
              {timeline.map((ev) => {
                const tone = domainContractEventTone(ev);
                const stageLabel = domainContractEventLabel(ev);
                const title = String(ev?.humanReadable?.title || stageLabel).trim() || stageLabel;
                const description = String(ev?.humanReadable?.description || '').trim();
                const ts = Number(ev?.ts || 0);
                const timeLabel = ts > 0
                  ? new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
                  : '';
                return (
                  <div
                    key={String(ev?.id || `${ev?.type}-${ev?.ts}`)}
                    style={{
                      padding: '5px 7px',
                      borderRadius: 8,
                      border: toneBorder(tone),
                      background: toneBg(tone, theme),
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6 }}>
                      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-heading)' }}>{title}</div>
                      {timeLabel && (
                        <div style={{ fontSize: 9, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>{timeLabel}</div>
                      )}
                    </div>
                    {description && (
                      <div style={{ marginTop: 2, fontSize: 10, color: 'var(--text-secondary-strong)' }}>{description}</div>
                    )}
                  </div>
                );
              })}
            </div>
          </AccordionSection>
        )}
      </div>
    </div>
  );
};

/* ------------------------------------------------------------------ */
/*  Sub-components                                                    */
/* ------------------------------------------------------------------ */

const AccordionSection: React.FC<{
  id: string;
  label: string;
  expanded: boolean;
  onToggle: (id: string) => void;
  theme: string;
  maxBodyHeight?: number;
  bodyPadding?: string;
  children: React.ReactNode;
}> = ({
  id,
  label,
  expanded,
  onToggle,
  theme,
  children,
  maxBodyHeight = SECTION_MAX_HEIGHT,
  bodyPadding = '4px 10px 8px',
}) => (
  <div
    style={{
      display: 'flex',
      flexDirection: 'column',
      minHeight: 0,
      borderRadius: 10,
      border: '1px solid var(--border-subtle)',
      background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)',
      overflow: 'hidden',
    }}
  >
    <button
      type="button"
      onClick={() => onToggle(id)}
      style={{
        display: 'flex',
        alignItems: 'center',
        width: '100%',
        padding: '7px 10px',
        border: 'none',
        background: 'transparent',
        cursor: 'pointer',
        gap: 6,
      }}
    >
      <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)', transition: 'transform 0.15s', transform: expanded ? 'rotate(90deg)' : 'none' }}>
        ▸
      </span>
      <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-heading)' }}>{label}</span>
    </button>
    {expanded && (
      <div
        style={{
          padding: bodyPadding,
          maxHeight: maxBodyHeight,
          overflowY: 'auto',
          overscrollBehavior: 'contain',
        }}
      >
        {children}
      </div>
    )}
  </div>
);

const ScoreBar: React.FC<{ score: number; theme: string }> = ({ score, theme }) => {
  const filled = Math.max(0, Math.min(5, Math.round(score * 5)));
  return (
    <div style={{ display: 'flex', gap: 2, flexShrink: 0 }} title={`Score: ${(score * 100).toFixed(0)}%`}>
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          style={{
            width: 5,
            height: 5,
            borderRadius: 999,
            background: i < filled
              ? (theme === 'dark' ? '#38BDF8' : '#0284C7')
              : (theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(0,0,0,0.08)'),
          }}
        />
      ))}
    </div>
  );
};
