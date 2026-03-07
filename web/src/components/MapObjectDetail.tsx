import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { GeoObject } from '../hooks/useGeoObjects';

interface MapObjectDetailProps {
  object: GeoObject;
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  edgesLoading: boolean;
  edgesCount: number;
  onClose: () => void;
}

export const MapObjectDetail: React.FC<MapObjectDetailProps> = ({
  object, isDark, paletteTheme, edgesLoading, edgesCount, onClose,
}) => {
  const visualType = getHorizonNodeVisualType(object.type, object.canonicalPath, object.label, object.facilityKind);
  const accent = getNodeColors(visualType || object.type, paletteTheme).accent;
  const facilityKindLabel = String(object.facilityKind || '')
    .trim()
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
  const typeLabel = object.type === 'FACILITY' && facilityKindLabel
    ? `Facility (${facilityKindLabel})`
    : formatHorizonFragmentType(object.type, object.canonicalPath, object.label);
  const isAffectedZone = object.type === 'AFFECTED_ZONE';
  const zoneTypeLabel = String(object.zoneType || '').trim().replace(/_/g, ' ');
  const zoneSubTypeLabel = String(object.zoneSubType || '').trim().replace(/_/g, ' ');
  const zoneStatus = String(object.activityStatus || '').trim();
  const zoneHype = Number(object.hype);
  const zoneLastMatchedAt = object.lastMatchedAt
    ? new Date(object.lastMatchedAt).toLocaleString()
    : '';

  return (
    <div
      style={{
        position: 'absolute',
        left: 260,
        top: 10,
        maxWidth: 340,
        padding: '10px 12px',
        borderRadius: 10,
        backgroundColor: isDark ? 'rgba(6, 14, 22, 0.9)' : 'rgba(255, 255, 255, 0.95)',
        border: isDark ? '1px solid rgba(255,255,255,0.14)' : '1px solid rgba(15,23,42,0.2)',
        color: 'var(--text-heading)',
        boxShadow: isDark ? '0 8px 30px rgba(0,0,0,0.35)' : '0 8px 30px rgba(15,23,42,0.12)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <HorizonNodeIcon type={visualType || object.type} color={accent} size={14} />
        <div style={{ fontSize: 13, fontWeight: 700 }}>
          {object.label || object.canonicalPath}
        </div>
      </div>
      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 2 }}>
        {typeLabel}
      </div>
      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
        {object.canonicalPath}
      </div>
      {object.description && (
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
          {object.description}
        </div>
      )}
      {isAffectedZone && (
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
          {zoneTypeLabel && (
            <div>Zone type: {zoneTypeLabel}</div>
          )}
          {zoneSubTypeLabel && (
            <div>Subtype: {zoneSubTypeLabel}</div>
          )}
          {(zoneStatus || Number.isFinite(zoneHype)) && (
            <div>
              Activity: {zoneStatus || 'unknown'}
              {Number.isFinite(zoneHype) ? ` • Hype ${Math.round(zoneHype)}/100` : ''}
            </div>
          )}
          {zoneLastMatchedAt && (
            <div>Last match: {zoneLastMatchedAt}</div>
          )}
        </div>
      )}
      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
        Status: {object.status || 'ACTIVE'}
        {object.inScope === false && (
          <span style={{ marginLeft: 8, opacity: 0.7 }}>(neighbor)</span>
        )}
      </div>
      {edgesLoading && (
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
          Loading connections…
        </div>
      )}
      {!edgesLoading && edgesCount > 0 && (
        <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
          Connections: {edgesCount}
        </div>
      )}
      <button
        type="button"
        className="sidebar-icon-btn"
        style={{ marginTop: 8 }}
        onClick={onClose}
        title="Close details"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 6 6 18" />
          <path d="M6 6l12 12" />
        </svg>
      </button>
    </div>
  );
};
