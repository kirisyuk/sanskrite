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
  pathLegsEnabled: boolean;
  pathLegCount: number;
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
  onTogglePathLegsEnabled: () => void;
  onToggleVesselsEnabled: () => void;
  onSelectAll: () => void;
  onSelectNone: () => void;
  onSelectAllZoneTypes: () => void;
  onSelectNoneZoneTypes: () => void;
  onSelectAllFlowModes: () => void;
  onSelectNoneFlowModes: () => void;
}

const HOPS_OPTIONS = [1, 2, 3] as const;
const DEFAULT_HOPS = 3;
const OBJECT_LIST_MAX_HEIGHT = 248;
const DETAIL_LIST_MAX_HEIGHT = 170;

type DatePresetId = 'all' | '7d' | '30d' | '90d' | 'custom';

const DATE_PRESETS: Array<{ id: Exclude<DatePresetId, 'custom'>; label: string; days: number | null }> = [
  { id: 'all', label: 'All', days: null },
  { id: '7d', label: '7d', days: 7 },
  { id: '30d', label: '30d', days: 30 },
  { id: '90d', label: '90d', days: 90 },
];

function formatInputDate(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function subtractDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() - days);
  return next;
}

function detectDatePreset(dateFrom: string, dateTo: string, today: Date): DatePresetId {
  if (!dateFrom && !dateTo) return 'all';
  const todayKey = formatInputDate(today);
  for (const preset of DATE_PRESETS) {
    if (preset.days === null) continue;
    const fromKey = formatInputDate(subtractDays(today, preset.days - 1));
    if (dateFrom === fromKey && dateTo === todayKey) return preset.id;
  }
  return 'custom';
}

function panelSurface(isDark: boolean): React.CSSProperties {
  return {
    position: 'absolute',
    right: 10,
    top: 10,
    width: 286,
    maxHeight: 'calc(100% - 20px)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
    padding: 12,
    borderRadius: 16,
    background: isDark
      ? 'linear-gradient(180deg, rgba(6, 18, 31, 0.96) 0%, rgba(3, 12, 22, 0.95) 100%)'
      : 'linear-gradient(180deg, rgba(255, 255, 255, 0.98) 0%, rgba(245, 248, 252, 0.96) 100%)',
    border: isDark ? '1px solid rgba(148, 163, 184, 0.2)' : '1px solid rgba(15, 23, 42, 0.14)',
    boxShadow: isDark ? '0 18px 38px rgba(0, 0, 0, 0.42)' : '0 16px 36px rgba(15, 23, 42, 0.16)',
    backdropFilter: 'blur(16px)',
    color: 'var(--text-heading)',
    fontSize: 11,
  };
}

function sectionSurface(isDark: boolean): React.CSSProperties {
  return {
    borderRadius: 12,
    padding: 10,
    background: isDark ? 'rgba(255, 255, 255, 0.04)' : 'rgba(15, 23, 42, 0.035)',
    border: isDark ? '1px solid rgba(148, 163, 184, 0.12)' : '1px solid rgba(15, 23, 42, 0.08)',
  };
}

function actionButtonStyle(isDark: boolean, emphasized = false): React.CSSProperties {
  return {
    border: 'none',
    cursor: 'pointer',
    borderRadius: 999,
    padding: emphasized ? '6px 10px' : '4px 8px',
    fontSize: 10,
    fontWeight: 700,
    color: emphasized
      ? (isDark ? '#dbeafe' : '#1d4ed8')
      : (isDark ? '#93c5fd' : '#2563eb'),
    background: emphasized
      ? (isDark ? 'rgba(37, 99, 235, 0.18)' : 'rgba(37, 99, 235, 0.12)')
      : 'transparent',
  };
}

function countBadgeStyle(isDark: boolean): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 28,
    padding: '3px 8px',
    borderRadius: 999,
    background: isDark ? 'rgba(148, 163, 184, 0.12)' : 'rgba(15, 23, 42, 0.06)',
    color: 'var(--text-secondary-strong)',
    fontSize: 10,
    fontWeight: 700,
    fontVariantNumeric: 'tabular-nums',
  };
}

const ToggleSwitch: React.FC<{
  checked: boolean;
  isDark: boolean;
  activeTint?: string;
  onClick: () => void;
}> = ({ checked, isDark, activeTint, onClick }) => (
  <button
    type="button"
    onClick={onClick}
    aria-pressed={checked}
    style={{
      position: 'relative',
      width: 40,
      height: 24,
      border: 'none',
      borderRadius: 999,
      cursor: 'pointer',
      padding: 0,
      background: checked
        ? activeTint || (isDark ? 'rgba(56, 189, 248, 0.42)' : 'rgba(37, 99, 235, 0.24)')
        : (isDark ? 'rgba(148, 163, 184, 0.2)' : 'rgba(15, 23, 42, 0.12)'),
      transition: 'background 160ms ease',
      flexShrink: 0,
    }}
  >
    <span
      style={{
        position: 'absolute',
        top: 3,
        left: checked ? 19 : 3,
        width: 18,
        height: 18,
        borderRadius: 999,
        background: checked ? '#f8fafc' : (isDark ? 'rgba(226, 232, 240, 0.84)' : '#ffffff'),
        boxShadow: checked ? '0 2px 8px rgba(15, 23, 42, 0.28)' : '0 1px 4px rgba(15, 23, 42, 0.18)',
        transition: 'left 160ms ease, background 160ms ease',
      }}
    />
  </button>
);

const SectionHeader: React.FC<{
  title: string;
  subtitle?: string;
  countText?: string;
  isDark: boolean;
  open: boolean;
  onToggle: () => void;
  action?: React.ReactNode;
}> = ({ title, subtitle, countText, isDark, open, onToggle, action }) => (
  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: open ? 10 : 0 }}>
    <button
      type="button"
      onClick={onToggle}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        flex: 1,
        background: 'none',
        border: 'none',
        padding: 0,
        textAlign: 'left',
        cursor: 'pointer',
        color: 'inherit',
      }}
    >
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12, fontWeight: 700, letterSpacing: '0.01em' }}>{title}</div>
        {(subtitle || countText) && (
          <div style={{ marginTop: 2, fontSize: 10, color: 'var(--text-secondary-strong)' }}>
            {subtitle}
            {subtitle && countText ? ' . ' : ''}
            {countText}
          </div>
        )}
      </div>
      <span
        style={{
          ...countBadgeStyle(isDark),
          minWidth: 34,
          padding: '3px 10px',
          color: isDark ? '#cbd5e1' : '#334155',
        }}
      >
        {open ? 'Hide' : 'Show'}
      </span>
    </button>
    {action}
  </div>
);

const SummaryChip: React.FC<{
  label: string;
  value: string;
  isDark: boolean;
}> = ({ label, value, isDark }) => (
  <div
    style={{
      display: 'inline-flex',
      alignItems: 'center',
      gap: 6,
      padding: '5px 9px',
      borderRadius: 999,
      background: isDark ? 'rgba(148, 163, 184, 0.1)' : 'rgba(15, 23, 42, 0.05)',
      border: isDark ? '1px solid rgba(148, 163, 184, 0.12)' : '1px solid rgba(15, 23, 42, 0.06)',
      color: 'var(--text-secondary-strong)',
      fontSize: 10,
    }}
  >
    <span>{label}</span>
    <strong style={{ color: 'var(--text-heading)', fontWeight: 700 }}>{value}</strong>
  </div>
);

const FilterRowButton: React.FC<{
  active: boolean;
  isDark: boolean;
  accent?: string;
  icon?: React.ReactNode;
  label: string;
  count: number;
  onClick: () => void;
}> = ({ active, isDark, accent, icon, label, count, onClick }) => (
  <button
    type="button"
    onClick={onClick}
    style={{
      display: 'flex',
      alignItems: 'center',
      gap: 9,
      width: '100%',
      padding: '6px 8px',
      borderRadius: 10,
      border: 'none',
      cursor: 'pointer',
      textAlign: 'left',
      background: active
        ? (isDark ? 'rgba(148, 163, 184, 0.11)' : 'rgba(15, 23, 42, 0.055)')
        : 'transparent',
      opacity: active ? 1 : 0.48,
      transition: 'opacity 140ms ease, background 140ms ease',
    }}
  >
    {icon ? (
      <span style={{ width: 14, display: 'inline-flex', justifyContent: 'center', flexShrink: 0 }}>{icon}</span>
    ) : (
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: 999,
          background: accent || 'currentColor',
          flexShrink: 0,
        }}
      />
    )}
    <span
      style={{
        flex: 1,
        minWidth: 0,
        fontSize: 11,
        color: 'var(--text-heading)',
        whiteSpace: 'nowrap',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}
    >
      {label}
    </span>
    <span
      style={{
        minWidth: 28,
        textAlign: 'right',
        fontSize: 10,
        fontVariantNumeric: 'tabular-nums',
        color: 'var(--text-secondary-strong)',
      }}
    >
      {count}
    </span>
  </button>
);

const LayerRow: React.FC<{
  title: string;
  subtitle: string;
  countText?: string;
  enabled: boolean;
  isDark: boolean;
  activeTint?: string;
  detailAvailable?: boolean;
  detailOpen?: boolean;
  onToggleEnabled: () => void;
  onToggleDetails?: () => void;
}> = ({
  title,
  subtitle,
  countText,
  enabled,
  isDark,
  activeTint,
  detailAvailable,
  detailOpen,
  onToggleEnabled,
  onToggleDetails,
}) => (
  <div
    style={{
      display: 'flex',
      alignItems: 'center',
      gap: 10,
      padding: '8px 10px',
      borderRadius: 12,
      background: isDark ? 'rgba(255, 255, 255, 0.045)' : 'rgba(15, 23, 42, 0.035)',
      border: isDark ? '1px solid rgba(148, 163, 184, 0.1)' : '1px solid rgba(15, 23, 42, 0.08)',
    }}
  >
    <div style={{ flex: 1, minWidth: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 11, fontWeight: 600 }}>{title}</span>
        {countText && <span style={{ ...countBadgeStyle(isDark), minWidth: 0 }}>{countText}</span>}
      </div>
      <div style={{ marginTop: 2, fontSize: 10, color: 'var(--text-secondary-strong)' }}>{subtitle}</div>
    </div>
    {detailAvailable && onToggleDetails && (
      <button type="button" onClick={onToggleDetails} style={actionButtonStyle(isDark)}>
        {detailOpen ? 'Hide' : 'Edit'}
      </button>
    )}
    <ToggleSwitch checked={enabled} isDark={isDark} activeTint={activeTint} onClick={onToggleEnabled} />
  </div>
);

export const MapFilterPanel: React.FC<MapFilterPanelProps> = ({
  isDark,
  paletteTheme,
  typeCounts,
  zoneTypeCounts,
  flowModeCounts,
  activeTypes,
  activeZoneTypes,
  activeFlowModes,
  zonesEnabled,
  flowsEnabled,
  pathLegsEnabled,
  pathLegCount,
  vesselsEnabled,
  vesselsApiAvailable,
  dateFrom,
  dateTo,
  hops,
  loadingData,
  onSetDateFrom,
  onSetDateTo,
  onSetHops,
  onToggleType,
  onToggleZoneType,
  onToggleFlowMode,
  onToggleZonesEnabled,
  onToggleFlowsEnabled,
  onTogglePathLegsEnabled,
  onToggleVesselsEnabled,
  onSelectAll,
  onSelectNone,
  onSelectAllZoneTypes,
  onSelectNoneZoneTypes,
  onSelectAllFlowModes,
  onSelectNoneFlowModes,
}) => {
  const [quickControlsOpen, setQuickControlsOpen] = React.useState(true);
  const [layersOpen, setLayersOpen] = React.useState(true);
  const [objectsOpen, setObjectsOpen] = React.useState(true);
  const [zoneDetailsOpen, setZoneDetailsOpen] = React.useState(false);
  const [flowDetailsOpen, setFlowDetailsOpen] = React.useState(false);
  const [objectSearch, setObjectSearch] = React.useState('');
  const today = React.useMemo(() => new Date(), []);
  const isAllSelected = activeTypes === null;
  const isAllZoneTypesSelected = activeZoneTypes === null;
  const isAllFlowModesSelected = activeFlowModes === null;
  const totalTypeCount = typeCounts.length;
  const totalZoneTypeCount = zoneTypeCounts.length;
  const totalFlowModeCount = flowModeCounts.length;
  const selectedTypeCount = isAllSelected ? totalTypeCount : activeTypes.size;
  const selectedZoneTypeCount = isAllZoneTypesSelected ? totalZoneTypeCount : activeZoneTypes.size;
  const selectedFlowModeCount = isAllFlowModesSelected ? totalFlowModeCount : activeFlowModes.size;
  const activeDatePreset = detectDatePreset(dateFrom, dateTo, today);
  const objectQuery = objectSearch.trim().toLowerCase();
  const filteredTypeCounts = React.useMemo(() => {
    if (!objectQuery) return typeCounts;
    return typeCounts.filter(({ type }) => {
      const label = formatHorizonFragmentType(type, '', '').toLowerCase();
      return label.includes(objectQuery) || type.toLowerCase().includes(objectQuery);
    });
  }, [objectQuery, typeCounts]);
  const hasAnyFilters =
    dateFrom !== '' ||
    dateTo !== '' ||
    hops !== DEFAULT_HOPS ||
    activeTypes !== null ||
    activeZoneTypes !== null ||
    activeFlowModes !== null ||
    zonesEnabled ||
    flowsEnabled ||
    pathLegsEnabled ||
    vesselsEnabled;

  React.useEffect(() => {
    if (zonesEnabled) setZoneDetailsOpen(true);
  }, [zonesEnabled]);

  React.useEffect(() => {
    if (flowsEnabled) setFlowDetailsOpen(true);
  }, [flowsEnabled]);

  const handleSelectDatePreset = React.useCallback((days: number | null) => {
    if (days === null) {
      onSetDateFrom('');
      onSetDateTo('');
      return;
    }
    const to = formatInputDate(today);
    const from = formatInputDate(subtractDays(today, days - 1));
    onSetDateFrom(from);
    onSetDateTo(to);
  }, [onSetDateFrom, onSetDateTo, today]);

  const handleResetAll = React.useCallback(() => {
    if (dateFrom) onSetDateFrom('');
    if (dateTo) onSetDateTo('');
    if (hops !== DEFAULT_HOPS) onSetHops(DEFAULT_HOPS);
    if (activeTypes !== null) onSelectAll();
    if (activeZoneTypes !== null) onSelectAllZoneTypes();
    if (activeFlowModes !== null) onSelectAllFlowModes();
    if (zonesEnabled) onToggleZonesEnabled();
    if (flowsEnabled) onToggleFlowsEnabled();
    if (pathLegsEnabled) onTogglePathLegsEnabled();
    if (vesselsEnabled) onToggleVesselsEnabled();
    setObjectSearch('');
  }, [
    activeFlowModes,
    activeTypes,
    activeZoneTypes,
    dateFrom,
    dateTo,
    flowsEnabled,
    hops,
    onSelectAll,
    onSelectAllFlowModes,
    onSelectAllZoneTypes,
    onSetDateFrom,
    onSetDateTo,
    onSetHops,
    onToggleFlowsEnabled,
    onTogglePathLegsEnabled,
    onToggleVesselsEnabled,
    pathLegsEnabled,
    onToggleZonesEnabled,
    vesselsEnabled,
    zonesEnabled,
  ]);

  const summaryItems = [
    { label: 'Types', value: `${selectedTypeCount}/${totalTypeCount || 0}` },
    { label: 'Zones', value: zonesEnabled ? `${selectedZoneTypeCount}/${totalZoneTypeCount || 0}` : 'Off' },
    { label: 'Flows', value: flowsEnabled ? `${selectedFlowModeCount}/${totalFlowModeCount || 0}` : 'Off' },
    { label: 'Paths', value: pathLegsEnabled ? `${pathLegCount}` : 'Off' },
    { label: 'Hops', value: `${hops}` },
  ];
  if (dateFrom || dateTo) {
    summaryItems.push({ label: 'Window', value: activeDatePreset === 'custom' ? 'Custom' : activeDatePreset.toUpperCase() });
  }
  if (vesselsApiAvailable) {
    summaryItems.push({ label: 'AIS', value: vesselsEnabled ? 'On' : 'Off' });
  }

  return (
    <div style={panelSurface(isDark)}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, marginBottom: 12 }}>
        <div>
          <div style={{ fontWeight: 700, fontSize: 14, letterSpacing: '-0.01em' }}>Filters</div>
          <div style={{ marginTop: 2, fontSize: 10, color: 'var(--text-secondary-strong)' }}>
            Global controls, layers and object visibility.
          </div>
        </div>
        <button
          type="button"
          onClick={handleResetAll}
          disabled={!hasAnyFilters}
          style={{
            ...actionButtonStyle(isDark, true),
            opacity: hasAnyFilters ? 1 : 0.45,
            cursor: hasAnyFilters ? 'pointer' : 'default',
          }}
        >
          Reset all
        </button>
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginBottom: 12 }}>
        {summaryItems.map((item) => (
          <SummaryChip key={item.label} label={item.label} value={item.value} isDark={isDark} />
        ))}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12, overflowY: 'auto', paddingRight: 4, marginRight: -4 }}>
        <section style={sectionSurface(isDark)}>
          <SectionHeader
            title="Quick controls"
            subtitle="Core scope and time window"
            countText={`${activeDatePreset === 'custom' ? 'Custom date' : activeDatePreset.toUpperCase()} . ${hops} hops`}
            isDark={isDark}
            open={quickControlsOpen}
            onToggle={() => setQuickControlsOpen((v) => !v)}
          />
          {quickControlsOpen && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <div>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginBottom: 6 }}>Max hops</div>
                <div style={{ display: 'flex', gap: 6 }}>
                  {HOPS_OPTIONS.map((n) => (
                    <button
                      key={n}
                      type="button"
                      onClick={() => onSetHops(n)}
                      style={{
                        flex: 1,
                        padding: '7px 0',
                        borderRadius: 10,
                        border: 'none',
                        cursor: 'pointer',
                        fontSize: 11,
                        fontWeight: 700,
                        background: hops === n
                          ? (isDark ? 'rgba(59, 130, 246, 0.28)' : 'rgba(37, 99, 235, 0.14)')
                          : (isDark ? 'rgba(148, 163, 184, 0.1)' : 'rgba(15, 23, 42, 0.05)'),
                        color: hops === n ? (isDark ? '#bfdbfe' : '#1d4ed8') : 'var(--text-heading)',
                      }}
                    >
                      {n}
                    </button>
                  ))}
                </div>
              </div>

              <div>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginBottom: 6 }}>Date window</div>
                <div style={{ display: 'flex', gap: 6, marginBottom: 8 }}>
                  {DATE_PRESETS.map((preset) => {
                    const isActive = activeDatePreset === preset.id;
                    return (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => handleSelectDatePreset(preset.days)}
                        style={{
                          flex: 1,
                          padding: '6px 0',
                          borderRadius: 10,
                          border: 'none',
                          cursor: 'pointer',
                          fontSize: 10,
                          fontWeight: 700,
                          background: isActive
                            ? (isDark ? 'rgba(16, 185, 129, 0.22)' : 'rgba(5, 150, 105, 0.13)')
                            : (isDark ? 'rgba(148, 163, 184, 0.1)' : 'rgba(15, 23, 42, 0.05)'),
                          color: isActive ? (isDark ? '#a7f3d0' : '#047857') : 'var(--text-heading)',
                        }}
                      >
                        {preset.label}
                      </button>
                    );
                  })}
                </div>
                <div style={{ display: 'flex', gap: 6 }}>
                  <input
                    type="date"
                    value={dateFrom}
                    onChange={(e) => onSetDateFrom(e.target.value)}
                    style={{
                      flex: 1,
                      padding: '8px 9px',
                      borderRadius: 10,
                      border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.14)',
                      background: isDark ? 'rgba(2, 6, 23, 0.38)' : 'rgba(255, 255, 255, 0.88)',
                      color: 'var(--text-heading)',
                      fontSize: 11,
                    }}
                    placeholder="From"
                  />
                  <input
                    type="date"
                    value={dateTo}
                    onChange={(e) => onSetDateTo(e.target.value)}
                    style={{
                      flex: 1,
                      padding: '8px 9px',
                      borderRadius: 10,
                      border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.14)',
                      background: isDark ? 'rgba(2, 6, 23, 0.38)' : 'rgba(255, 255, 255, 0.88)',
                      color: 'var(--text-heading)',
                      fontSize: 11,
                    }}
                    placeholder="To"
                  />
                </div>
              </div>
            </div>
          )}
        </section>

        <section style={sectionSurface(isDark)}>
          <SectionHeader
            title="Layers"
            subtitle="Map overlays and dataset groups"
            countText={[
              zonesEnabled ? 'Zones on' : 'Zones off',
              flowsEnabled ? 'Flows on' : 'Flows off',
              pathLegsEnabled ? 'Paths on' : 'Paths off',
              vesselsApiAvailable ? (vesselsEnabled ? 'AIS on' : 'AIS off') : null,
            ].filter(Boolean).join(' . ')}
            isDark={isDark}
            open={layersOpen}
            onToggle={() => setLayersOpen((v) => !v)}
          />
          {layersOpen && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <LayerRow
                title="Static zones"
                subtitle="Industrial and strategic area overlays"
                countText={`${selectedZoneTypeCount}/${totalZoneTypeCount || 0}`}
                enabled={zonesEnabled}
                isDark={isDark}
                activeTint={isDark ? 'rgba(34, 197, 94, 0.38)' : 'rgba(22, 163, 74, 0.22)'}
                detailAvailable={zonesEnabled && totalZoneTypeCount > 0}
                detailOpen={zoneDetailsOpen}
                onToggleEnabled={onToggleZonesEnabled}
                onToggleDetails={() => setZoneDetailsOpen((v) => !v)}
              />

              {zonesEnabled && zoneDetailsOpen && zoneTypeCounts.length > 0 && (
                <div style={{ padding: '0 2px 0 4px' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                    <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>Zone categories</span>
                    <div style={{ display: 'flex', gap: 6 }}>
                      <button type="button" onClick={onSelectAllZoneTypes} style={actionButtonStyle(isDark)}>All</button>
                      <button type="button" onClick={onSelectNoneZoneTypes} style={actionButtonStyle(isDark)}>Clear</button>
                    </div>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2, maxHeight: DETAIL_LIST_MAX_HEIGHT, overflowY: 'auto', paddingRight: 2 }}>
                    {zoneTypeCounts.map(({ type, count }) => {
                      const isActive = isAllZoneTypesSelected || (activeZoneTypes?.has(type) ?? false);
                      return (
                        <FilterRowButton
                          key={type}
                          active={isActive}
                          isDark={isDark}
                          accent={zoneTypeColor(type, paletteTheme)}
                          label={formatZoneType(type)}
                          count={count}
                          onClick={() => onToggleZoneType(type)}
                        />
                      );
                    })}
                  </div>
                </div>
              )}

              {vesselsApiAvailable && (
                <LayerRow
                  title="Vessels (AIS)"
                  subtitle="Live maritime vessel signal"
                  enabled={vesselsEnabled}
                  isDark={isDark}
                  activeTint={isDark ? 'rgba(14, 165, 233, 0.38)' : 'rgba(3, 105, 161, 0.2)'}
                  onToggleEnabled={onToggleVesselsEnabled}
                />
              )}

              <LayerRow
                title="Transport flows"
                subtitle="Routes and corridor overlays"
                countText={`${selectedFlowModeCount}/${totalFlowModeCount || 0}`}
                enabled={flowsEnabled}
                isDark={isDark}
                activeTint={isDark ? 'rgba(244, 114, 182, 0.34)' : 'rgba(190, 24, 93, 0.2)'}
                detailAvailable={flowsEnabled && totalFlowModeCount > 0}
                detailOpen={flowDetailsOpen}
                onToggleEnabled={onToggleFlowsEnabled}
                onToggleDetails={() => setFlowDetailsOpen((v) => !v)}
              />

              <LayerRow
                title="Path legs"
                subtitle="Materialized supply-path segments"
                countText={pathLegCount > 0 ? `${pathLegCount}` : undefined}
                enabled={pathLegsEnabled}
                isDark={isDark}
                activeTint={isDark ? 'rgba(251, 191, 36, 0.34)' : 'rgba(217, 119, 6, 0.2)'}
                onToggleEnabled={onTogglePathLegsEnabled}
              />

              {flowsEnabled && flowDetailsOpen && flowModeCounts.length > 0 && (
                <div style={{ padding: '0 2px 0 4px' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                    <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>Flow modes</span>
                    <div style={{ display: 'flex', gap: 6 }}>
                      <button type="button" onClick={onSelectAllFlowModes} style={actionButtonStyle(isDark)}>All</button>
                      <button type="button" onClick={onSelectNoneFlowModes} style={actionButtonStyle(isDark)}>Clear</button>
                    </div>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2, maxHeight: DETAIL_LIST_MAX_HEIGHT, overflowY: 'auto', paddingRight: 2 }}>
                    {flowModeCounts.map(({ mode, count }) => {
                      const isActive = isAllFlowModesSelected || (activeFlowModes?.has(mode) ?? false);
                      return (
                        <FilterRowButton
                          key={mode}
                          active={isActive}
                          isDark={isDark}
                          accent={transportFlowModeColor(mode, paletteTheme)}
                          label={formatTransportFlowMode(mode)}
                          count={count}
                          onClick={() => onToggleFlowMode(mode)}
                        />
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          )}
        </section>

        <section style={sectionSurface(isDark)}>
          <SectionHeader
            title="Object types"
            subtitle="Fragment categories visible on the map"
            countText={`${selectedTypeCount}/${totalTypeCount || 0} selected`}
            isDark={isDark}
            open={objectsOpen}
            onToggle={() => setObjectsOpen((v) => !v)}
            action={
              objectsOpen ? (
                <div style={{ display: 'flex', gap: 6 }}>
                  <button type="button" onClick={onSelectAll} style={actionButtonStyle(isDark)}>All</button>
                  <button type="button" onClick={onSelectNone} style={actionButtonStyle(isDark)}>Clear</button>
                </div>
              ) : null
            }
          />

          {objectsOpen && (
            <>
              <div style={{ marginBottom: 10 }}>
                <input
                  type="search"
                  value={objectSearch}
                  onChange={(e) => setObjectSearch(e.target.value)}
                  placeholder="Filter object types"
                  style={{
                    width: '100%',
                    padding: '8px 10px',
                    borderRadius: 10,
                    border: isDark ? '1px solid rgba(148, 163, 184, 0.16)' : '1px solid rgba(15, 23, 42, 0.12)',
                    background: isDark ? 'rgba(2, 6, 23, 0.38)' : 'rgba(255, 255, 255, 0.88)',
                    color: 'var(--text-heading)',
                    fontSize: 11,
                  }}
                />
              </div>

              {typeCounts.length > 0 ? (
                filteredTypeCounts.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2, maxHeight: OBJECT_LIST_MAX_HEIGHT, overflowY: 'auto', paddingRight: 2 }}>
                    {filteredTypeCounts.map(({ type, count }) => {
                      const isActive = isAllSelected || (activeTypes?.has(type) ?? false);
                      const visualType = getHorizonNodeVisualType(type, '', '');
                      const accent = getNodeColors(visualType || type, paletteTheme).accent;
                      return (
                        <FilterRowButton
                          key={type}
                          active={isActive}
                          isDark={isDark}
                          icon={<HorizonNodeIcon type={visualType || type} color={accent} size={12} />}
                          label={formatHorizonFragmentType(type, '', '')}
                          count={count}
                          onClick={() => onToggleType(type)}
                        />
                      );
                    })}
                  </div>
                ) : (
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>No object types match this search.</div>
                )
              ) : (
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {loadingData ? 'Loading…' : 'No types yet'}
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  );
};
