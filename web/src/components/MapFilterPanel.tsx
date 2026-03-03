import React from 'react';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';
import type { TypeCount } from '../hooks/useGeoObjects';

interface MapFilterPanelProps {
  isDark: boolean;
  paletteTheme: 'dark' | 'light';
  typeCounts: TypeCount[];
  activeTypes: Set<string> | null;
  dateFrom: string;
  dateTo: string;
  loadingData: boolean;
  onSetDateFrom: (v: string) => void;
  onSetDateTo: (v: string) => void;
  onToggleType: (type: string) => void;
  onSelectAll: () => void;
  onSelectNone: () => void;
}

export const MapFilterPanel: React.FC<MapFilterPanelProps> = ({
  isDark, paletteTheme, typeCounts, activeTypes, dateFrom, dateTo, loadingData,
  onSetDateFrom, onSetDateTo, onToggleType, onSelectAll, onSelectNone,
}) => {
  const isAllSelected = activeTypes === null;

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
