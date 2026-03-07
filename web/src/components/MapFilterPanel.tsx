import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { TypeCount } from '../hooks/useGeoObjects';
import { formatZoneType, zoneTypeColor, type StaticZoneType, type ZoneTypeCount } from '../lib/staticZones';
import {
  formatTransportFlowMode,
  transportFlowModeColor,
  type TransportFlowMode,
  type TransportFlowModeCount,
} from '../lib/staticTransportFlows';

interface MapFilterPanelProps {
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  typeCounts: TypeCount[];
  zoneTypeCounts: ZoneTypeCount[];
  flowModeCounts: TransportFlowModeCount[];
  activeTypes: Set<string> | null;
  activeZoneTypes: Set<StaticZoneType> | null;
  activeFlowModes: Set<TransportFlowMode> | null;
  zonesEnabled: boolean;
  flowsEnabled: boolean;
  vesselsEnabled: boolean;
  vesselsApiAvailable: boolean;
  dateFrom: string;
  dateTo: string;
  hops: number;
  loadingData: boolean;
  onSetDateFrom: (v: string) => void;
  onSetDateTo: (v: string) => void;
  onSetHops: (v: number) => void;
  onToggleType: (type: string) => void;
  onToggleZoneType: (type: StaticZoneType) => void;
  onToggleFlowMode: (mode: TransportFlowMode) => void;
  onToggleZonesEnabled: () => void;
  onToggleFlowsEnabled: () => void;
  onToggleVesselsEnabled: () => void;
  onSelectAll: () => void;
  onSelectNone: () => void;
  onSelectAllZoneTypes: () => void;
  onSelectNoneZoneTypes: () => void;
  onSelectAllFlowModes: () => void;
  onSelectNoneFlowModes: () => void;
}

const HOPS_OPTIONS = [1, 2, 3, 4] as const;

export const MapFilterPanel: React.FC<MapFilterPanelProps> = ({
  isDark, paletteTheme, typeCounts, zoneTypeCounts, flowModeCounts, activeTypes, activeZoneTypes, activeFlowModes,   zonesEnabled, flowsEnabled, vesselsEnabled, vesselsApiAvailable, dateFrom, dateTo, hops, loadingData,
  onSetDateFrom, onSetDateTo, onSetHops, onToggleType, onToggleZoneType, onToggleFlowMode, onToggleZonesEnabled, onToggleFlowsEnabled, onToggleVesselsEnabled, onSelectAll, onSelectNone, onSelectAllZoneTypes, onSelectNoneZoneTypes, onSelectAllFlowModes, onSelectNoneFlowModes,
}) => {
  const isAllSelected = activeTypes === null;
  const isAllZoneTypesSelected = activeZoneTypes === null;
  const isAllFlowModesSelected = activeFlowModes === null;

  return (
    <div
      style={{
        position: 'absolute',
        left: 10,
        top: 10,
        width: 240,
        maxHeight: 'calc(100% - 100px)',
        overflowY: 'auto',
        padding: '12px 12px 10px',
        borderRadius: 10,
        backgroundColor: isDark ? 'rgba(6, 14, 22, 0.94)' : 'rgba(255,255,255,0.97)',
        border: isDark ? '1px solid rgba(255,255,255,0.18)' : '1px solid rgba(15,23,42,0.2)',
        boxShadow: isDark ? '0 8px 32px rgba(0,0,0,0.5)' : '0 8px 32px rgba(15,23,42,0.15)',
        color: 'var(--text-heading)',
        fontSize: 11,
      }}
    >
      <div style={{ fontWeight: 600, fontSize: 12, marginBottom: 10, letterSpacing: '0.02em' }}>Filters</div>

      <div style={{ marginBottom: 12 }}>
        <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginBottom: 4 }}>Dependency hops</div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          {HOPS_OPTIONS.map((n) => (
            <button
              key={n}
              type="button"
              onClick={() => onSetHops(n)}
              style={{
                padding: '6px 10px', borderRadius: 6, border: 'none', cursor: 'pointer', fontSize: 11,
                background: hops === n ? (isDark ? 'rgba(100,160,250,0.35)' : 'rgba(37,99,235,0.2)') : (isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)'),
                color: hops === n ? (isDark ? '#93c5fd' : '#2563eb') : 'var(--text-heading)',
              }}
            >
              {n}
            </button>
          ))}
        </div>
      </div>

      <div style={{ marginBottom: 12 }}>
        <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginBottom: 4 }}>Date range</div>
        <div style={{ display: 'flex', gap: 6 }}>
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => onSetDateFrom(e.target.value)}
            style={{
              flex: 1, padding: '6px 8px', borderRadius: 6,
              border: isDark ? '1px solid rgba(255,255,255,0.2)' : '1px solid rgba(15,23,42,0.25)',
              background: isDark ? 'rgba(0,0,0,0.3)' : 'rgba(255,255,255,0.9)',
              color: 'var(--text-heading)', fontSize: 11,
            }}
            placeholder="From"
          />
          <input
            type="date"
            value={dateTo}
            onChange={(e) => onSetDateTo(e.target.value)}
            style={{
              flex: 1, padding: '6px 8px', borderRadius: 6,
              border: isDark ? '1px solid rgba(255,255,255,0.2)' : '1px solid rgba(15,23,42,0.25)',
              background: isDark ? 'rgba(0,0,0,0.3)' : 'rgba(255,255,255,0.9)',
              color: 'var(--text-heading)', fontSize: 11,
            }}
            placeholder="To"
          />
        </div>
      </div>

      <div style={{ marginBottom: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
          <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>... zones</span>
          <button
            type="button"
            onClick={onToggleZonesEnabled}
            style={{
              padding: '4px 8px',
              borderRadius: 999,
              border: 'none',
              cursor: 'pointer',
              fontSize: 10,
              fontWeight: 700,
              background: zonesEnabled
                ? (isDark ? 'rgba(56,189,248,0.3)' : 'rgba(3,105,161,0.16)')
                : (isDark ? 'rgba(255,255,255,0.1)' : 'rgba(15,23,42,0.08)'),
              color: zonesEnabled ? (isDark ? '#bae6fd' : '#0c4a6e') : 'var(--text-secondary-strong)',
            }}
          >
            {zonesEnabled ? 'On' : 'Off'}
          </button>
        </div>

        {zonesEnabled && zoneTypeCounts.length > 0 && (
          <>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6, marginBottom: 4 }}>
              <button
                type="button"
                onClick={onSelectAllZoneTypes}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: 10,
                  padding: '2px 4px',
                  borderRadius: 4,
                  color: isAllZoneTypesSelected ? 'var(--text-secondary-strong)' : (isDark ? '#60a5fa' : '#2563eb'),
                }}
              >
                All
              </button>
              <button
                type="button"
                onClick={onSelectNoneZoneTypes}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: 10,
                  padding: '2px 4px',
                  borderRadius: 4,
                  color: isDark ? '#60a5fa' : '#2563eb',
                }}
              >
                None
              </button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              {zoneTypeCounts.map(({ type, count }) => {
                const isActive = isAllZoneTypesSelected || (activeZoneTypes?.has(type) ?? false);
                return (
                  <button
                    key={type}
                    type="button"
                    onClick={() => onToggleZoneType(type)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 7,
                      padding: '4px 6px',
                      borderRadius: 6,
                      border: 'none',
                      cursor: 'pointer',
                      textAlign: 'left',
                      width: '100%',
                      background: isActive ? (isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)') : 'transparent',
                      opacity: isActive ? 1 : 0.45,
                      transition: 'opacity 0.15s, background 0.15s',
                    }}
                  >
                    <span style={{ width: 8, height: 8, borderRadius: 999, background: zoneTypeColor(type, paletteTheme) }} />
                    <span style={{ flex: 1, fontSize: 11, color: 'var(--text-heading)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {formatZoneType(type)}
                    </span>
                    <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)', minWidth: 20, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                      {count}
                    </span>
                  </button>
                );
              })}
            </div>
          </>
        )}
      </div>

      {vesselsApiAvailable && (
        <div style={{ marginBottom: 12 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
            <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>Vessels (AIS)</span>
            <button
              type="button"
              onClick={onToggleVesselsEnabled}
              style={{
                padding: '4px 8px',
                borderRadius: 999,
                border: 'none',
                cursor: 'pointer',
                fontSize: 10,
                fontWeight: 700,
                background: vesselsEnabled
                  ? (isDark ? 'rgba(56,189,248,0.3)' : 'rgba(3,105,161,0.16)')
                  : (isDark ? 'rgba(255,255,255,0.1)' : 'rgba(15,23,42,0.08)'),
                color: vesselsEnabled ? (isDark ? '#bae6fd' : '#0c4a6e') : 'var(--text-secondary-strong)',
              }}
            >
              {vesselsEnabled ? 'On' : 'Off'}
            </button>
          </div>
        </div>
      )}

      <div style={{ marginBottom: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
          <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>... flows</span>
          <button
            type="button"
            onClick={onToggleFlowsEnabled}
            style={{
              padding: '4px 8px',
              borderRadius: 999,
              border: 'none',
              cursor: 'pointer',
              fontSize: 10,
              fontWeight: 700,
              background: flowsEnabled
                ? (isDark ? 'rgba(248,113,113,0.25)' : 'rgba(190,24,93,0.14)')
                : (isDark ? 'rgba(255,255,255,0.1)' : 'rgba(15,23,42,0.08)'),
              color: flowsEnabled ? (isDark ? '#fecaca' : '#881337') : 'var(--text-secondary-strong)',
            }}
          >
            {flowsEnabled ? 'On' : 'Off'}
          </button>
        </div>

        {flowsEnabled && flowModeCounts.length > 0 && (
          <>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6, marginBottom: 4 }}>
              <button
                type="button"
                onClick={onSelectAllFlowModes}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: 10,
                  padding: '2px 4px',
                  borderRadius: 4,
                  color: isAllFlowModesSelected ? 'var(--text-secondary-strong)' : (isDark ? '#60a5fa' : '#2563eb'),
                }}
              >
                All
              </button>
              <button
                type="button"
                onClick={onSelectNoneFlowModes}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  fontSize: 10,
                  padding: '2px 4px',
                  borderRadius: 4,
                  color: isDark ? '#60a5fa' : '#2563eb',
                }}
              >
                None
              </button>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              {flowModeCounts.map(({ mode, count }) => {
                const isActive = isAllFlowModesSelected || (activeFlowModes?.has(mode) ?? false);
                return (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => onToggleFlowMode(mode)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 7,
                      padding: '4px 6px',
                      borderRadius: 6,
                      border: 'none',
                      cursor: 'pointer',
                      textAlign: 'left',
                      width: '100%',
                      background: isActive ? (isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)') : 'transparent',
                      opacity: isActive ? 1 : 0.45,
                      transition: 'opacity 0.15s, background 0.15s',
                    }}
                  >
                    <span style={{ width: 8, height: 8, borderRadius: 999, background: transportFlowModeColor(mode, paletteTheme) }} />
                    <span style={{ flex: 1, fontSize: 11, color: 'var(--text-heading)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {formatTransportFlowMode(mode)}
                    </span>
                    <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)', minWidth: 20, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                      {count}
                    </span>
                  </button>
                );
              })}
            </div>
          </>
        )}
      </div>

      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
          <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>Object types</span>
          <div style={{ display: 'flex', gap: 6 }}>
            <button
              type="button"
              onClick={onSelectAll}
              style={{
                background: 'none', border: 'none', cursor: 'pointer', fontSize: 10, padding: '2px 4px', borderRadius: 4,
                color: isAllSelected ? 'var(--text-secondary-strong)' : (isDark ? '#60a5fa' : '#2563eb'),
              }}
            >All</button>
            <button
              type="button"
              onClick={onSelectNone}
              style={{
                background: 'none', border: 'none', cursor: 'pointer', fontSize: 10, padding: '2px 4px', borderRadius: 4,
                color: isDark ? '#60a5fa' : '#2563eb',
              }}
            >None</button>
          </div>
        </div>
        {typeCounts.length > 0 ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {typeCounts.map(({ type, count }) => {
              const isActive = isAllSelected || (activeTypes?.has(type) ?? false);
              const visualType = getHorizonNodeVisualType(type, '', '');
              const accent = getNodeColors(visualType || type, paletteTheme).accent;
              return (
                <button
                  key={type}
                  type="button"
                  onClick={() => onToggleType(type)}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 7, padding: '4px 6px', borderRadius: 6,
                    border: 'none', cursor: 'pointer', textAlign: 'left', width: '100%',
                    background: isActive ? (isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)') : 'transparent',
                    opacity: isActive ? 1 : 0.45,
                    transition: 'opacity 0.15s, background 0.15s',
                  }}
                >
                  <HorizonNodeIcon type={visualType || type} color={accent} size={12} />
                  <span style={{ flex: 1, fontSize: 11, color: 'var(--text-heading)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {formatHorizonFragmentType(type, '', '')}
                  </span>
                  <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)', minWidth: 20, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                    {count}
                  </span>
                </button>
              );
            })}
          </div>
        ) : (
          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
            {loadingData ? 'Loading…' : 'No types yet'}
          </div>
        )}
      </div>
    </div>
  );
};
