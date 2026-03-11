import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { GeoObject } from '../hooks/useGeoObjects';
import type { GeoEdgeTraceSummary } from '../hooks/useGeoEdgeLayer';
import { geoCodeToShortName, humanizeCanonicalPath } from '../lib/uiUtils';

interface MapObjectDetailProps {
  object: GeoObject;
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  edgesLoading: boolean;
  edgesCount: number;
  visibleEdgesCount: number;
  traceSummary: GeoEdgeTraceSummary | null;
  onClose: () => void;
}

function formatFacilityKind(value?: string): string {
  return String(value || '')
    .trim()
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function extractCountryLabel(canonicalPath?: string): string {
  const path = String(canonicalPath || '').trim().toLowerCase();
  if (!path) return '';
  const parts = path.split('/').filter(Boolean);
  if (parts.length < 2) return '';
  const namespace = parts[0];
  if (!['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community', 'zone'].includes(namespace)) return '';
  const codes = String(parts[1] || '')
    .split('-')
    .map((value) => value.trim().toLowerCase())
    .filter((value) => value.length === 2 && /^[a-z]{2}$/.test(value) && value !== 'na');
  if (codes.length === 0) return '';
  return codes.map((code) => geoCodeToShortName(code)).join(' / ');
}

function getObjectDisplayName(object: GeoObject): string {
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath;
}

function humanizeToken(value?: string): string {
  return String(value || '')
    .trim()
    .toLowerCase()
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function compactRole(value?: string): string {
  return String(value || '')
    .trim()
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function familyAccent(family: string, isDark: boolean): string {
  switch (family) {
    case 'supply':
      return isDark ? '#fbbf24' : '#b45309';
    case 'control':
      return isDark ? '#93c5fd' : '#2563eb';
    case 'risk':
    case 'influence':
      return isDark ? '#fb7185' : '#be123c';
    case 'location':
    case 'market':
      return isDark ? '#2dd4bf' : '#0f766e';
    case 'social':
      return isDark ? '#c4b5fd' : '#7c3aed';
    default:
      return isDark ? '#cbd5e1' : '#64748b';
  }
}

function sectionCardStyle(isDark: boolean): React.CSSProperties {
  return {
    padding: '10px 11px',
    borderRadius: 12,
    background: isDark ? 'rgba(148, 163, 184, 0.08)' : 'rgba(15, 23, 42, 0.035)',
    border: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.06)',
  };
}

export const MapObjectDetail: React.FC<MapObjectDetailProps> = ({
  object,
  isDark,
  paletteTheme,
  edgesLoading,
  edgesCount,
  visibleEdgesCount,
  traceSummary,
  onClose,
}) => {
  const [viewport, setViewport] = React.useState(() => ({
    width: typeof window === 'undefined' ? 1440 : window.innerWidth,
    height: typeof window === 'undefined' ? 900 : window.innerHeight,
  }));
  const autoCompact = viewport.width < 1280 || viewport.height < 760;
  const [isCollapsed, setIsCollapsed] = React.useState(autoCompact);

  React.useEffect(() => {
    if (typeof window === 'undefined') return;
    const handleResize = () => setViewport({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  React.useEffect(() => {
    setIsCollapsed(autoCompact);
  }, [autoCompact, object.id]);

  const visualType = getHorizonNodeVisualType(object.type, object.canonicalPath, object.label, object.facilityKind);
  const accent = getNodeColors(visualType || object.type, paletteTheme).accent;
  const facilityKindLabel = formatFacilityKind(object.facilityKind);
  const typeLabel = object.type === 'FACILITY' && facilityKindLabel
    ? `Facility (${facilityKindLabel})`
    : formatHorizonFragmentType(object.type, object.canonicalPath, object.label);
  const countryLabel = extractCountryLabel(object.canonicalPath);

  const chips = [
    { label: 'Status', value: object.status || 'ACTIVE' },
    edgesLoading ? { label: 'Direct', value: 'Loading…' } : { label: 'Direct', value: String(traceSummary?.directCount ?? edgesCount) },
    !edgesLoading ? { label: 'Context', value: String(traceSummary?.contextCount ?? 0) } : null,
    !edgesLoading ? { label: 'Paths', value: String(traceSummary?.supplyPathCount ?? 0) } : null,
    !edgesLoading && visibleEdgesCount > 0 ? { label: 'Map lines', value: String(traceSummary?.visibleLineCount ?? visibleEdgesCount) } : null,
    traceSummary && traceSummary.stressedSupplyPathCount > 0 ? { label: 'Stress', value: String(traceSummary.stressedSupplyPathCount) } : null,
  ].filter(Boolean) as Array<{ label: string; value: string }>;

  const overlayWidth = autoCompact ? 314 : 384;
  const overlayMaxWidth = viewport.width < 980 ? 'calc(100% - 20px)' : 'calc(100% - 340px)';

  return (
    <div
      style={{
        position: 'absolute',
        left: 10,
        top: 10,
        width: overlayWidth,
        maxWidth: overlayMaxWidth,
        maxHeight: 'calc(100% - 20px)',
        overflow: 'auto',
        zIndex: 20,
        borderRadius: 18,
        background: isDark
          ? 'linear-gradient(180deg, rgba(6,18,31,0.97) 0%, rgba(3,12,22,0.96) 100%)'
          : 'linear-gradient(180deg, rgba(255,255,255,0.985) 0%, rgba(245,248,252,0.97) 100%)',
        border: isDark ? '1px solid rgba(148,163,184,0.22)' : '1px solid rgba(15,23,42,0.14)',
        boxShadow: isDark ? '0 18px 44px rgba(0,0,0,0.44)' : '0 16px 40px rgba(15,23,42,0.16)',
        backdropFilter: 'blur(18px)',
        color: 'var(--text-heading)',
      }}
    >
      <div style={{ position: 'absolute', inset: 0, width: 3, background: accent }} />

      <button
        type="button"
        onClick={() => setIsCollapsed((value) => !value)}
        aria-label={isCollapsed ? 'Expand fragment details' : 'Collapse fragment details'}
        style={{
          position: 'absolute',
          top: 10,
          right: 38,
          width: 24,
          height: 24,
          borderRadius: 999,
          border: 'none',
          cursor: 'pointer',
          display: 'grid',
          placeItems: 'center',
          fontSize: 15,
          lineHeight: 1,
          color: isDark ? 'rgba(226,232,240,0.88)' : 'rgba(15,23,42,0.74)',
          background: isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.06)',
        }}
      >
        {isCollapsed ? '+' : '−'}
      </button>

      <button
        type="button"
        onClick={onClose}
        aria-label="Close fragment details"
        style={{
          position: 'absolute',
          top: 10,
          right: 10,
          width: 24,
          height: 24,
          borderRadius: 999,
          border: 'none',
          cursor: 'pointer',
          display: 'grid',
          placeItems: 'center',
          fontSize: 15,
          lineHeight: 1,
          color: isDark ? 'rgba(226,232,240,0.88)' : 'rgba(15,23,42,0.74)',
          background: isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.06)',
        }}
      >
        ×
      </button>

      <div style={{ padding: autoCompact ? '12px 44px 12px 14px' : '14px 44px 14px 14px', display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
          <div
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: autoCompact ? 26 : 30,
              height: autoCompact ? 26 : 30,
              borderRadius: 999,
              flexShrink: 0,
              background: isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.05)',
              border: isDark ? '1px solid rgba(148,163,184,0.16)' : '1px solid rgba(15,23,42,0.08)',
            }}
          >
            <HorizonNodeIcon type={visualType || object.type} color={accent} size={15} />
          </div>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div style={{ marginBottom: 3, fontSize: 10, fontWeight: 700, letterSpacing: '0.11em', textTransform: 'uppercase', color: accent }}>
              {typeLabel}
            </div>
            <div style={{ fontSize: autoCompact ? 15 : 18, lineHeight: 1.15, fontWeight: 750, letterSpacing: '-0.015em' }}>
              {getObjectDisplayName(object)}
            </div>
            {countryLabel && (
              <div style={{ marginTop: 4, fontSize: 12, lineHeight: 1.35, opacity: 0.74 }}>
                {countryLabel}
              </div>
            )}
          </div>
        </div>

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
          {chips.map((chip) => (
            <span
              key={`${chip.label}:${chip.value}`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                padding: '4px 8px',
                borderRadius: 999,
                fontSize: 10,
                lineHeight: 1,
                fontWeight: 700,
                background: isDark ? 'rgba(148,163,184,0.12)' : 'rgba(15,23,42,0.05)',
                border: isDark ? '1px solid rgba(148,163,184,0.16)' : '1px solid rgba(15,23,42,0.08)',
              }}
            >
              <strong>{chip.label}</strong>
              {chip.value}
            </span>
          ))}
        </div>

        {isCollapsed ? null : (
          <>
            <div style={sectionCardStyle(isDark)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: accent }}>
                Why It Matters
              </div>
              <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.5, fontWeight: 600 }}>
                {edgesLoading ? 'Resolving relationship logic, supply paths, and propagation around this object…' : (traceSummary?.summary || 'No explainable relationship summary yet.')}
              </div>
              {traceSummary && traceSummary.whyItMatters.length > 0 && (
                <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {traceSummary.whyItMatters.map((item) => (
                    <div key={`${item.title}:${item.body}`} style={{ paddingLeft: 10, borderLeft: `2px solid ${accent}` }}>
                      <div style={{ fontSize: 11, fontWeight: 750 }}>{item.title}</div>
                      <div style={{ marginTop: 2, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        {item.body}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div style={sectionCardStyle(isDark)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Direct Relations
              </div>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 8 }}>
                {traceSummary?.directRelations.length ? traceSummary.directRelations.map((relation) => (
                  <div key={relation.edgeId || `${relation.relatedFragmentId}:${relation.type}`} style={{ display: 'flex', gap: 10 }}>
                    <div style={{ width: 8, flexShrink: 0, borderRadius: 999, background: familyAccent(relation.family, isDark) }} />
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 11, fontWeight: 700 }}>
                        {relation.relatedLabel}
                      </div>
                      <div style={{ marginTop: 1, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        {humanizeToken(relation.type)}
                        {relation.relatedType ? ` · ${humanizeToken(relation.relatedType)}` : ''}
                      </div>
                      {(relation.businessMeaning || relation.whyRelevant) && (
                        <div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                          {relation.businessMeaning ? `${relation.businessMeaning}. ` : ''}
                          {relation.whyRelevant || ''}
                        </div>
                      )}
                    </div>
                  </div>
                )) : (
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    No mapped direct relations for this object yet.
                  </div>
                )}
              </div>
            </div>

            <div style={sectionCardStyle(isDark)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Context
              </div>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 8 }}>
                {traceSummary?.contextRelations.length ? traceSummary.contextRelations.map((relation) => (
                  <div key={relation.edgeId || `${relation.relatedFragmentId}:${relation.type}`} style={{ display: 'flex', gap: 10 }}>
                    <div style={{ width: 8, flexShrink: 0, borderRadius: 999, background: familyAccent(relation.family, isDark) }} />
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 11, fontWeight: 700 }}>
                        {relation.relatedLabel}
                      </div>
                      <div style={{ marginTop: 1, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                        {humanizeToken(relation.type)}
                        {relation.semanticDirection ? ` · ${humanizeToken(relation.semanticDirection)}` : ''}
                      </div>
                      {(relation.businessMeaning || relation.whyRelevant) && (
                        <div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                          {relation.businessMeaning ? `${relation.businessMeaning}. ` : ''}
                          {relation.whyRelevant || ''}
                        </div>
                      )}
                    </div>
                  </div>
                )) : (
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    No mapped context anchors for this object yet.
                  </div>
                )}
              </div>
            </div>

            <div style={sectionCardStyle(isDark)}>
              <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                Supply Chain Paths
              </div>
              <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 8 }}>
                {traceSummary?.supplyPaths.length ? traceSummary.supplyPaths.map((path) => (
                  <div key={path.id} style={{ padding: '8px 9px', borderRadius: 10, background: isDark ? 'rgba(245, 158, 11, 0.1)' : 'rgba(245, 158, 11, 0.08)', border: isDark ? '1px solid rgba(245, 158, 11, 0.18)' : '1px solid rgba(180, 83, 9, 0.14)' }}>
                    <div style={{ fontSize: 11, fontWeight: 700 }}>
                      {path.title}
                    </div>
                    <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                      {compactRole(path.role)}
                      {path.productLabel ? ` · ${path.productLabel}` : ''}
                      {path.legCount > 0 ? ` · ${path.legCount} leg${path.legCount > 1 ? 's' : ''}` : ''}
                    </div>
                    <div style={{ marginTop: 4, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                      {path.reason}
                    </div>
                    <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                      {path.isPinned && (
                        <span style={{ fontSize: 10, padding: '3px 7px', borderRadius: 999, background: isDark ? 'rgba(14,165,233,0.16)' : 'rgba(14,165,233,0.1)' }}>
                          Pinned
                        </span>
                      )}
                      {path.stressScore >= 0.58 && (
                        <span style={{ fontSize: 10, padding: '3px 7px', borderRadius: 999, background: isDark ? 'rgba(248,113,113,0.16)' : 'rgba(239,68,68,0.1)' }}>
                          Stress {Math.round(path.stressScore * 100)}
                        </span>
                      )}
                      {path.hasBottleneck && (
                        <span style={{ fontSize: 10, padding: '3px 7px', borderRadius: 999, background: isDark ? 'rgba(251,146,60,0.16)' : 'rgba(249,115,22,0.1)' }}>
                          Bottleneck
                        </span>
                      )}
                    </div>
                  </div>
                )) : (
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    No concrete supply paths are linked to this object yet.
                  </div>
                )}
              </div>
            </div>

            {(traceSummary?.propagationEdgeCount || 0) > 0 && (
              <div style={sectionCardStyle(isDark)}>
                <div style={{ fontSize: 10, fontWeight: 800, letterSpacing: '0.09em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                  Propagation
                </div>
                <div style={{ marginTop: 6, fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                  Signal propagation is shown separately in gray across {traceSummary?.propagationEdgeCount || 0} link(s) touching {traceSummary?.propagationNodeCount || 0} node(s).
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};
