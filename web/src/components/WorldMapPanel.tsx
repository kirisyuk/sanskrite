import React from 'react';
import { useMapInstance } from '../hooks/useMapInstance';
import { useGeoObjects, type GeoObject } from '../hooks/useGeoObjects';
import { useGeoMarkerLayer } from '../hooks/useGeoMarkerLayer';
import { useGeoZoneLayer } from '../hooks/useGeoZoneLayer';
import { useGeoEdgeLayer } from '../hooks/useGeoEdgeLayer';
import { useStaticZoneLayer } from '../hooks/useStaticZoneLayer';
import { useTransportFlowLayer } from '../hooks/useTransportFlowLayer';
import { MapFilterPanel } from './MapFilterPanel';
import { MapObjectDetail } from './MapObjectDetail';
import { STATIC_ZONE_DATA, deriveZoneTypeCounts, type StaticZoneFeature, type StaticZoneType } from '../lib/staticZones';
import {
  STATIC_TRANSPORT_FLOW_DATA,
  deriveTransportFlowModeCounts,
  type TransportFlowFeature,
  type TransportFlowMode,
} from '../lib/staticTransportFlows';

export interface WorldMapPanelProps {
  scope?: { lensId: string };
}

export const WorldMapPanel: React.FC<WorldMapPanelProps> = ({ scope }) => {
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  const { map, isReady, styleEpoch, theme } = useMapInstance(containerRef);
  const isDark = theme === 'dark';

  const [dateFrom, setDateFrom] = React.useState('');
  const [dateTo, setDateTo] = React.useState('');
  const [activeTypes, setActiveTypes] = React.useState<Set<string> | null>(null);
  const [zonesEnabled, setZonesEnabled] = React.useState(false);
  const [activeZoneTypes, setActiveZoneTypes] = React.useState<Set<StaticZoneType> | null>(null);
  const [flowsEnabled, setFlowsEnabled] = React.useState(false);
  const [activeFlowModes, setActiveFlowModes] = React.useState<Set<TransportFlowMode> | null>(null);
  const [hops, setHops] = React.useState(3);
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);

  const filters = React.useMemo(
    () => ({ dateFrom, dateTo, activeTypes }),
    [dateFrom, dateTo, activeTypes],
  );

  const { allObjects, filteredObjects, typeCounts, loading, error } = useGeoObjects(scope, filters);

  const { edgesLoading, edgesCount, discoveredObjects } = useGeoEdgeLayer(map, isReady, styleEpoch, selectedObject, filteredObjects, hops);
  const mapObjects = React.useMemo(
    () => (discoveredObjects.length > 0 ? [...filteredObjects, ...discoveredObjects] : filteredObjects),
    [filteredObjects, discoveredObjects],
  );
  const allStaticZones = STATIC_ZONE_DATA.features;
  const zoneTypeCounts = React.useMemo(() => deriveZoneTypeCounts(allStaticZones), [allStaticZones]);
  const filteredStaticZones = React.useMemo(() => {
    if (!zonesEnabled) return [] as StaticZoneFeature[];
    if (!activeZoneTypes) return allStaticZones;
    return allStaticZones.filter((f) => activeZoneTypes.has(f.properties.zoneType));
  }, [zonesEnabled, activeZoneTypes, allStaticZones]);
  const allTransportFlows = STATIC_TRANSPORT_FLOW_DATA.features;
  const flowModeCounts = React.useMemo(() => deriveTransportFlowModeCounts(allTransportFlows), [allTransportFlows]);
  const filteredTransportFlows = React.useMemo(() => {
    if (!flowsEnabled) return [] as TransportFlowFeature[];
    if (!activeFlowModes) return allTransportFlows;
    return allTransportFlows.filter((f) => activeFlowModes.has(f.properties.flowMode));
  }, [flowsEnabled, activeFlowModes, allTransportFlows]);

  useGeoMarkerLayer(map, isReady, styleEpoch, mapObjects, theme, setSelectedObject);
  useGeoZoneLayer(map, isReady, styleEpoch, mapObjects, theme);
  useStaticZoneLayer(map, isReady, styleEpoch, filteredStaticZones, theme, zonesEnabled);
  useTransportFlowLayer(map, isReady, styleEpoch, filteredTransportFlows, theme, flowsEnabled);

  const handleToggleType = React.useCallback((type: string) => {
    setActiveTypes((prev) => {
      if (!prev) {
        const all = new Set(typeCounts.map((tc) => tc.type));
        all.delete(type);
        return all;
      }
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      if (next.size === typeCounts.length) return null;
      return next;
    });
  }, [typeCounts]);

  const filterActive = activeTypes !== null;
  const visibleStaticZonesCount = filteredStaticZones.length;
  const visibleTransportFlowsCount = filteredTransportFlows.length;

  const handleToggleZoneType = React.useCallback((type: StaticZoneType) => {
    setActiveZoneTypes((prev) => {
      if (!prev) {
        const all = new Set(zoneTypeCounts.map((zc) => zc.type));
        all.delete(type);
        return all;
      }
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      if (next.size === zoneTypeCounts.length) return null;
      return next;
    });
  }, [zoneTypeCounts]);

  const handleToggleFlowMode = React.useCallback((mode: TransportFlowMode) => {
    setActiveFlowModes((prev) => {
      if (!prev) {
        const all = new Set(flowModeCounts.map((fc) => fc.mode));
        all.delete(mode);
        return all;
      }
      const next = new Set(prev);
      if (next.has(mode)) next.delete(mode);
      else next.add(mode);
      if (next.size === flowModeCounts.length) return null;
      return next;
    });
  }, [flowModeCounts]);

  return (
    <div className="flex-1 min-h-0" style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ position: 'relative', flex: 1, minHeight: 0 }}>
        <div ref={containerRef} style={{ width: '100%', height: '100%', minHeight: 280 }} />

        {(loading || !isReady) && (
          <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center', color: 'var(--text-secondary-strong)', fontSize: 13, background: 'rgba(0,0,0,0.2)' }}>
            Loading geo objects...
          </div>
        )}

        {error && (
          <div style={{ position: 'absolute', left: 10, right: 10, bottom: 10, padding: '8px 10px', borderRadius: 8, border: '1px solid rgba(253,164,175,0.45)', backgroundColor: 'rgba(127,29,29,0.35)', color: '#fecdd3', fontSize: 12 }}>
            {error}
          </div>
        )}

        <MapFilterPanel
          isDark={isDark}
          paletteTheme={theme}
          typeCounts={typeCounts}
          zoneTypeCounts={zoneTypeCounts}
          flowModeCounts={flowModeCounts}
          activeTypes={activeTypes}
          activeZoneTypes={activeZoneTypes}
          activeFlowModes={activeFlowModes}
          zonesEnabled={zonesEnabled}
          flowsEnabled={flowsEnabled}
          dateFrom={dateFrom}
          dateTo={dateTo}
          hops={hops}
          loadingData={loading}
          onSetDateFrom={setDateFrom}
          onSetDateTo={setDateTo}
          onSetHops={setHops}
          onToggleType={handleToggleType}
          onToggleZoneType={handleToggleZoneType}
          onToggleFlowMode={handleToggleFlowMode}
          onToggleZonesEnabled={() => setZonesEnabled((v) => !v)}
          onToggleFlowsEnabled={() => setFlowsEnabled((v) => !v)}
          onSelectAll={() => setActiveTypes(null)}
          onSelectNone={() => setActiveTypes(new Set())}
          onSelectAllZoneTypes={() => setActiveZoneTypes(null)}
          onSelectNoneZoneTypes={() => setActiveZoneTypes(new Set())}
          onSelectAllFlowModes={() => setActiveFlowModes(null)}
          onSelectNoneFlowModes={() => setActiveFlowModes(new Set())}
        />

        <div style={{
          position: 'absolute', left: 10, bottom: 10, padding: '6px 10px', borderRadius: 8,
          backgroundColor: isDark ? 'rgba(5, 11, 17, 0.78)' : 'rgba(248, 250, 252, 0.92)',
          border: isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.15)',
          color: 'var(--text-secondary-strong)', fontSize: 11,
        }}>
          {filterActive
            ? `Shown: ${filteredObjects.length} / ${allObjects.length}`
            : `Objects: ${allObjects.length}`}
          {zonesEnabled ? ` | Zones: ${visibleStaticZonesCount}` : ''}
          {flowsEnabled ? ` | Flows: ${visibleTransportFlowsCount}` : ''}
        </div>

        {selectedObject && (
          <MapObjectDetail
            object={selectedObject}
            isDark={isDark}
            paletteTheme={theme}
            edgesLoading={edgesLoading}
            edgesCount={edgesCount}
            onClose={() => setSelectedObject(null)}
          />
        )}
      </div>
    </div>
  );
};

export default WorldMapPanel;
