import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { GeoObject } from '../hooks/useGeoObjects';
import { geoCodeToShortName, humanizeCanonicalPath } from '../lib/uiUtils';

interface MapObjectStackPanelProps {
  objects: GeoObject[];
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  priorityObjectId?: string | null;
  onClose: () => void;
  onSelectObject: (object: GeoObject) => void;
  onShowFan?: () => void;
}

function getObjectDisplayName(object: GeoObject): string {
  return String(object.label || '').trim() || humanizeCanonicalPath(object.canonicalPath) || object.canonicalPath;
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

export const MapObjectStackPanel: React.FC<MapObjectStackPanelProps> = ({
  objects,
  isDark,
  paletteTheme,
  priorityObjectId,
  onClose,
  onSelectObject,
  onShowFan,
}) => {
  const sortedObjects = React.useMemo(
    () =>
      [...objects].sort((a, b) => {
        const priorityDelta = Number(b.id === priorityObjectId) - Number(a.id === priorityObjectId);
        if (priorityDelta !== 0) return priorityDelta;
        const scopeDelta = Number(b.inScope !== false) - Number(a.inScope !== false);
        if (scopeDelta !== 0) return scopeDelta;
        return getObjectDisplayName(a).localeCompare(getObjectDisplayName(b));
      }),
    [objects, priorityObjectId],
  );

  return (
    <div
      style={{
        position: 'absolute',
        left: 10,
        top: 10,
        width: 360,
        maxWidth: 'calc(100% - 20px)',
        maxHeight: 'min(70vh, 620px)',
        zIndex: 20,
        display: 'flex',
        flexDirection: 'column',
        borderRadius: 16,
        overflow: 'hidden',
        background: isDark
          ? 'linear-gradient(180deg, rgba(6, 18, 31, 0.97) 0%, rgba(3, 12, 22, 0.95) 100%)'
          : 'linear-gradient(180deg, rgba(255, 255, 255, 0.99) 0%, rgba(245, 248, 252, 0.97) 100%)',
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
          background: isDark ? 'rgba(125, 211, 252, 0.9)' : 'rgba(37, 99, 235, 0.82)',
        }}
      />

      <button
        type="button"
        onClick={onClose}
        aria-label="Close object list"
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

      <div style={{ padding: '14px 44px 12px 14px', borderBottom: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.08)' }}>
        <div style={{ marginBottom: 4, fontSize: 10, fontWeight: 700, letterSpacing: '0.11em', textTransform: 'uppercase', color: isDark ? 'rgba(125, 211, 252, 0.88)' : 'rgba(37, 99, 235, 0.86)' }}>
          Shared Map Position
        </div>
        <div style={{ fontSize: 18, lineHeight: 1.15, fontWeight: 700, letterSpacing: '-0.01em' }}>
          {objects.length} objects at this point
        </div>
        <div style={{ marginTop: 6, fontSize: 12, lineHeight: 1.45, opacity: 0.74 }}>
          Choose by name instead of icon. Use fan-out only if you need spatial separation.
        </div>
        {onShowFan && (
          <button
            type="button"
            onClick={onShowFan}
            style={{
              marginTop: 10,
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 6,
              padding: '7px 11px',
              borderRadius: 999,
              border: isDark ? '1px solid rgba(125, 211, 252, 0.26)' : '1px solid rgba(37, 99, 235, 0.18)',
              background: isDark ? 'rgba(14, 116, 144, 0.16)' : 'rgba(219, 234, 254, 0.84)',
              color: isDark ? 'rgba(186, 230, 253, 0.94)' : 'rgba(29, 78, 216, 0.92)',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 700,
              letterSpacing: '0.01em',
            }}
          >
            Show fan-out on map
          </button>
        )}
      </div>

      <div style={{ overflowY: 'auto', padding: 10, display: 'flex', flexDirection: 'column', gap: 8 }}>
        {sortedObjects.map((object) => {
          const visualType = getHorizonNodeVisualType(object.type, object.canonicalPath, object.label, object.facilityKind);
          const accent = getNodeColors(visualType || object.type, paletteTheme).accent;
          const countryLabel = extractCountryLabel(object.canonicalPath);
          const typeLabel = formatHorizonFragmentType(object.type, object.canonicalPath, object.label);
          const statusLabel = String(object.status || object.activityStatus || '').trim();

          return (
            <button
              key={object.id}
              type="button"
              onClick={() => onSelectObject(object)}
              style={{
                width: '100%',
                display: 'flex',
                alignItems: 'flex-start',
                gap: 10,
                padding: '10px 12px',
                borderRadius: 14,
                textAlign: 'left',
                cursor: 'pointer',
                border: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.08)',
                background: isDark ? 'rgba(8, 21, 35, 0.72)' : 'rgba(255, 255, 255, 0.85)',
                color: 'inherit',
              }}
            >
              <div
                style={{
                  width: 30,
                  height: 30,
                  borderRadius: 999,
                  flexShrink: 0,
                  display: 'grid',
                  placeItems: 'center',
                  background: isDark ? 'rgba(148, 163, 184, 0.1)' : 'rgba(15, 23, 42, 0.05)',
                  border: isDark ? '1px solid rgba(148, 163, 184, 0.14)' : '1px solid rgba(15, 23, 42, 0.08)',
                }}
              >
                <HorizonNodeIcon type={visualType || object.type} color={accent} size={16} />
              </div>

              <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ marginBottom: 3, fontSize: 10, fontWeight: 700, letterSpacing: '0.09em', textTransform: 'uppercase', color: accent }}>
                  {typeLabel}
                </div>
                <div style={{ fontSize: 14, lineHeight: 1.25, fontWeight: 700, wordBreak: 'break-word' }}>
                  {getObjectDisplayName(object)}
                </div>
                {(countryLabel || statusLabel || object.inScope === false) && (
                  <div style={{ marginTop: 5, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                    {object.id === priorityObjectId && (
                      <span style={{ fontSize: 11, lineHeight: 1.2, opacity: 0.88, color: accent }}>
                        Search match
                      </span>
                    )}
                    {countryLabel && (
                      <span style={{ fontSize: 11, lineHeight: 1.2, opacity: 0.72 }}>
                        {countryLabel}
                      </span>
                    )}
                    {statusLabel && (
                      <span style={{ fontSize: 11, lineHeight: 1.2, opacity: 0.72 }}>
                        {statusLabel}
                      </span>
                    )}
                    {object.inScope === false && (
                      <span style={{ fontSize: 11, lineHeight: 1.2, opacity: 0.72 }}>
                        Neighbor
                      </span>
                    )}
                  </div>
                )}
                {object.id === priorityObjectId && !(countryLabel || statusLabel || object.inScope === false) && (
                  <div style={{ marginTop: 5 }}>
                    <span style={{ fontSize: 11, lineHeight: 1.2, opacity: 0.88, color: accent }}>
                      Search match
                    </span>
                  </div>
                )}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
};

export default MapObjectStackPanel;
