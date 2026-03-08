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

function formatFamilyLabel(value: GeoEdgeTraceSummary['topFamilies'][number]['family']): string {
  switch (value) {
    case 'supply': return 'Supply';
    case 'control': return 'Control';
    case 'influence': return 'Influence';
    case 'location': return 'Location';
    case 'market': return 'Market';
    case 'risk': return 'Risk';
    case 'social': return 'Social';
    default: return 'Other';
  }
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
    const handleResize = () => {
      setViewport({ width: window.innerWidth, height: window.innerHeight });
    };
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
  const zoneTypeLabel = String(object.zoneType || '').trim().replace(/_/g, ' ');
  const zoneSubTypeLabel = String(object.zoneSubType || '').trim().replace(/_/g, ' ');
  const zoneStatus = String(object.activityStatus || '').trim();
  const zoneHype = Number(object.hype);
  const zoneLastMatchedAt = object.lastMatchedAt
    ? new Date(object.lastMatchedAt).toLocaleString()
    : '';
  const countryLabel = extractCountryLabel(object.canonicalPath);

  const chips = [
    { label: 'Status', value: object.status || 'ACTIVE' },
    edgesLoading ? { label: 'Links', value: 'Loading...' } : (edgesCount > 0 ? { label: 'Links', value: String(edgesCount) } : null),
    !edgesLoading && visibleEdgesCount > 0 && visibleEdgesCount < edgesCount ? { label: 'Mapped', value: String(visibleEdgesCount) } : null,
    object.inScope === false ? { label: 'Scope', value: 'Neighbor' } : null,
    zoneStatus ? { label: 'Activity', value: zoneStatus } : null,
    Number.isFinite(zoneHype) ? { label: 'Hype', value: `${Math.round(zoneHype)}/100` } : null,
  ].filter(Boolean) as Array<{ label: string; value: string }>;

  const detailLines = [
    object.description || '',
    object.type === 'AFFECTED_ZONE' && zoneTypeLabel ? `Zone type: ${zoneTypeLabel}` : '',
    object.type === 'AFFECTED_ZONE' && zoneSubTypeLabel ? `Subtype: ${zoneSubTypeLabel}` : '',
    object.type === 'AFFECTED_ZONE' && zoneLastMatchedAt ? `Last match: ${zoneLastMatchedAt}` : '',
  ].filter(Boolean);

  const overlayWidth = autoCompact ? 286 : 338;
  const overlayMaxWidth = viewport.width < 980 ? 'calc(100% - 20px)' : 'calc(100% - 332px)';
  const headerGap = autoCompact ? 8 : 10;
  const traceChips = traceSummary
    ? [
        { label: '1 hop', value: String(traceSummary.hopCounts[1]) },
        { label: '2 hops', value: String(traceSummary.hopCounts[2]) },
        { label: '3 hops', value: String(traceSummary.hopCounts[3]) },
        { label: 'Branches', value: String(traceSummary.branchCount) },
        { label: 'Paths', value: String(traceSummary.pathArtifactCount) },
        { label: 'Legs', value: String(traceSummary.pathLegCount) },
        { label: 'Signals', value: String(traceSummary.propagationEdgeCount) },
      ]
    : [];

  return (
    <div
      style={{
        position: 'absolute',
        left: 10,
        top: 10,
        width: overlayWidth,
        maxWidth: overlayMaxWidth,
        zIndex: 20,
        borderRadius: 16,
        overflow: 'hidden',
        background: isDark
          ? 'linear-gradient(180deg, rgba(6, 18, 31, 0.96) 0%, rgba(3, 12, 22, 0.95) 100%)'
          : 'linear-gradient(180deg, rgba(255, 255, 255, 0.98) 0%, rgba(245, 248, 252, 0.96) 100%)',
        border: isDark ? '1px solid rgba(148, 163, 184, 0.22)' : '1px solid rgba(15, 23, 42, 0.14)',
        boxShadow: isDark ? '0 18px 38px rgba(0, 0, 0, 0.42)' : '0 16px 36px rgba(15, 23, 42, 0.16)',
        backdropFilter: 'blur(16px)',
        color: 'var(--text-heading)',
      }}
    >
      <div
        style={{
          position: 'absolute',
          inset: 0,
          width: 3,
          background: accent,
        }}
      />

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
          color: isDark ? 'rgba(226, 232, 240, 0.88)' : 'rgba(15, 23, 42, 0.74)',
          background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.06)',
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
          color: isDark ? 'rgba(226, 232, 240, 0.88)' : 'rgba(15, 23, 42, 0.74)',
          background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.06)',
        }}
      >
        ×
      </button>

      <div style={{ padding: autoCompact ? '12px 44px 12px 14px' : '14px 44px 14px 14px' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', gap: headerGap, marginBottom: isCollapsed ? 0 : 8 }}>
          <div
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: autoCompact ? 26 : 28,
              height: autoCompact ? 26 : 28,
              borderRadius: 999,
              flexShrink: 0,
              background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.05)',
              border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.08)',
            }}
          >
            <HorizonNodeIcon type={visualType || object.type} color={accent} size={15} />
          </div>

          <div style={{ minWidth: 0, flex: 1 }}>
            <div
              style={{
                marginBottom: 3,
                fontSize: 10,
                fontWeight: 700,
                letterSpacing: '0.11em',
                textTransform: 'uppercase',
                color: accent,
              }}
            >
              {typeLabel}
            </div>
            <div style={{ fontSize: autoCompact ? 15 : 17, lineHeight: 1.15, fontWeight: 700, letterSpacing: '-0.01em' }}>
              {getObjectDisplayName(object)}
            </div>
            {!isCollapsed && countryLabel && (
              <div style={{ marginTop: 4, fontSize: 12, lineHeight: 1.35, opacity: 0.72, wordBreak: 'break-word' }}>
                {countryLabel}
              </div>
            )}
          </div>
        </div>

        {isCollapsed ? (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 10 }}>
            {chips.slice(0, autoCompact ? 2 : 3).map((chip) => (
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
                  fontWeight: 600,
                  letterSpacing: '0.01em',
                  background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.05)',
                  border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.08)',
                }}
              >
                <strong style={{ fontSize: 10, fontWeight: 700 }}>{chip.label}</strong>
                {chip.value}
              </span>
            ))}
          </div>
        ) : detailLines.length > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 12 }}>
            {detailLines.map((line) => (
              <div key={line} style={{ fontSize: 12, lineHeight: 1.5, opacity: 0.86 }}>
                {line}
              </div>
            ))}
          </div>
        )}

        {!isCollapsed && (
          <>
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
                    fontWeight: 600,
                    letterSpacing: '0.01em',
                    background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.05)',
                    border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.08)',
                  }}
                >
                  <strong style={{ fontSize: 10, fontWeight: 700 }}>{chip.label}</strong>
                  {chip.value}
                </span>
              ))}
            </div>

            {(edgesLoading || traceSummary) && (
              <div
                style={{
                  marginTop: 12,
                  paddingTop: 12,
                  borderTop: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.08)',
                }}
              >
                <div style={{ fontSize: 10, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary-strong)' }}>
                  Trace propagation
                </div>
                <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.45, opacity: 0.8 }}>
                  {edgesLoading
                    ? 'Resolving direct and downstream branches...'
                    : traceSummary
                      ? (
                        traceSummary.visibleEdges > 0 && traceSummary.visibleEdges < traceSummary.totalEdges
                          ? `Showing ${traceSummary.totalEdges} relations across ${traceSummary.branchCount} active branches. ${traceSummary.visibleEdges} are visible on map.`
                          : traceSummary.visibleEdges === 0
                            ? `Showing ${traceSummary.totalEdges} relations across ${traceSummary.branchCount} active branches. None are mappable on the current map yet.`
                            : `Showing ${traceSummary.totalEdges} visible relations across ${traceSummary.branchCount} active branches.`
                      )
                      : 'No trace available yet.'}
                </div>
                {!!traceSummary && (
                  <>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 8 }}>
                      {traceChips.map((chip) => (
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
                            fontWeight: 600,
                            letterSpacing: '0.01em',
                            background: isDark ? 'rgba(37, 99, 235, 0.12)' : 'rgba(37, 99, 235, 0.08)',
                            border: isDark ? '1px solid rgba(96, 165, 250, 0.16)' : '1px solid rgba(37, 99, 235, 0.12)',
                          }}
                        >
                          <strong style={{ fontSize: 10, fontWeight: 700 }}>{chip.label}</strong>
                          {chip.value}
                        </span>
                      ))}
                    </div>
                    {traceSummary.topFamilies.length > 0 && (
                      <div style={{ marginTop: 8, fontSize: 11, lineHeight: 1.45, color: 'var(--text-secondary-strong)' }}>
                        Dominant semantics: {traceSummary.topFamilies.map((item) => `${formatFamilyLabel(item.family)} (${item.count})`).join(' . ')}
                      </div>
                    )}
                    {(traceSummary.pathArtifactCount > 0 || traceSummary.propagationNodeCount > 0) && (
                      <div style={{ marginTop: 8, fontSize: 11, lineHeight: 1.5, color: 'var(--text-secondary-strong)' }}>
                        {traceSummary.pathArtifactCount > 0
                          ? `Related paths ${traceSummary.pathArtifactCount}, mapped legs ${traceSummary.pathLegCount}. `
                          : ''}
                        {traceSummary.propagationNodeCount > 0
                          ? `Propagation preview reaches ${traceSummary.propagationNodeCount} node(s) across ${traceSummary.propagationEdgeCount} link(s).`
                          : ''}
                      </div>
                    )}
                    {traceSummary.countryHighlights.length > 0 && (
                      <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 8 }}>
                        {traceSummary.countryHighlights.map((country) => (
                          <div
                            key={country.countryIso2 || country.countryName}
                            style={{
                              padding: '8px 9px',
                              borderRadius: 10,
                              background: isDark ? 'rgba(148, 163, 184, 0.08)' : 'rgba(15, 23, 42, 0.035)',
                              border: isDark ? '1px solid rgba(148, 163, 184, 0.12)' : '1px solid rgba(15, 23, 42, 0.06)',
                            }}
                          >
                            <div style={{ fontSize: 11, fontWeight: 700 }}>
                              {country.countryName}
                              <span style={{ marginLeft: 6, color: 'var(--text-secondary-strong)', fontWeight: 600 }}>
                                {country.itemCount}
                              </span>
                            </div>
                            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 6 }}>
                              {country.items.map((item) => (
                                <div key={item.objectId} style={{ fontSize: 11, lineHeight: 1.4 }}>
                                  <div style={{ fontWeight: 600 }}>{item.label}</div>
                                  <div style={{ color: 'var(--text-secondary-strong)' }}>
                                    {item.type} . {item.hop} hop . {item.relationLabel}
                                  </div>
                                  <div style={{ color: 'var(--text-secondary-strong)', opacity: 0.88 }}>
                                    {item.businessMeaning}
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};
