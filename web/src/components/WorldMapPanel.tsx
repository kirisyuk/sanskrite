import React from 'react';
import { useMapInstance } from '../hooks/useMapInstance';
import { useGeoObjects, type GeoObject } from '../hooks/useGeoObjects';
import { useGeoMarkerLayer } from '../hooks/useGeoMarkerLayer';
import { useGeoZoneLayer } from '../hooks/useGeoZoneLayer';
import { useGeoEdgeLayer } from '../hooks/useGeoEdgeLayer';
import { useStaticZoneLayer } from '../hooks/useStaticZoneLayer';
import { useTransportFlowLayer } from '../hooks/useTransportFlowLayer';
import { usePathLegLayer } from '../hooks/usePathLegLayer';
import { useVesselLayer, vesselsApiAvailable as isVesselsApiAvailable } from '../hooks/useVesselLayer';
import { MapFilterPanel } from './MapFilterPanel';
import { MapObjectDetail } from './MapObjectDetail';
import { MapObjectStackPanel } from './MapObjectStackPanel';
import { STATIC_ZONE_DATA, deriveZoneTypeCounts, type StaticZoneFeature, type StaticZoneType } from '../lib/staticZones';
import {
  STATIC_TRANSPORT_FLOW_DATA,
  deriveTransportFlowModeCounts,
  type TransportFlowFeature,
  type TransportFlowMode,
} from '../lib/staticTransportFlows';
import { api } from '../lib/api';
import type { MaterializedArtifact } from './SupplyChainOverlay';
import type { GeoMarkerSelection } from '../hooks/useGeoMarkerLayer';

export interface WorldMapPanelProps {
  scope?: { lensId: string };
}

type MapActivityPayload = Awaited<ReturnType<typeof api.getHorizonMapActivity>>;
type ZoneActivityItem = MapActivityPayload['zones'][number];
type FlowActivityItem = MapActivityPayload['flows'][number];
type MapObjectStackSelection = Extract<GeoMarkerSelection, { kind: 'stack' }>;

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
  const [pathLegsEnabled, setPathLegsEnabled] = React.useState(Boolean(scope?.lensId));
  const [activeFlowModes, setActiveFlowModes] = React.useState<Set<TransportFlowMode> | null>(null);
  const [vesselsEnabled, setVesselsEnabled] = React.useState(false);
  const [hops, setHops] = React.useState(3);
  const vesselsApiAvailable = isVesselsApiAvailable();
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);
  const [stackSelection, setStackSelection] = React.useState<MapObjectStackSelection | null>(null);
  const [zoneActivityById, setZoneActivityById] = React.useState<Record<string, ZoneActivityItem>>({});
  const [flowActivityById, setFlowActivityById] = React.useState<Record<string, FlowActivityItem>>({});
  const [pathArtifacts, setPathArtifacts] = React.useState<MaterializedArtifact[]>([]);
  const stackSelectionRef = React.useRef<MapObjectStackSelection | null>(null);

  React.useEffect(() => {
    stackSelectionRef.current = stackSelection;
  }, [stackSelection]);

  const filters = React.useMemo(
    () => ({ dateFrom, dateTo, activeTypes }),
    [dateFrom, dateTo, activeTypes],
  );

  const { allObjects, filteredObjects, typeCounts, loading, error } = useGeoObjects(scope, filters);

  const { edgesLoading, edgesCount, visibleEdgesCount, discoveredObjects, traceSummary } = useGeoEdgeLayer(map, isReady, styleEpoch, selectedObject, filteredObjects, theme, hops);
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

  React.useEffect(() => {
    setPathLegsEnabled(Boolean(scope?.lensId));
    if (scope?.lensId) return;
    setPathArtifacts([]);
  }, [scope?.lensId]);

  React.useEffect(() => {
    if (!scope?.lensId) return;
    let cancelled = false;
    const load = async () => {
      try {
        const payload = await api.getSupplyChainsByLens(scope.lensId);
        if (!cancelled) {
          setPathArtifacts(Array.isArray(payload?.chains) ? (payload.chains as MaterializedArtifact[]) : []);
        }
      } catch {
        if (!cancelled) setPathArtifacts([]);
      }
    };
    void load();
    const timer = window.setInterval(() => { void load(); }, 60_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [scope?.lensId]);

  React.useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const payload = await api.getHorizonMapActivity({ lensId: scope?.lensId });
        if (cancelled) return;
        const z: Record<string, ZoneActivityItem> = {};
        const f: Record<string, FlowActivityItem> = {};
        for (const item of payload?.zones || []) {
          if (item?.id) z[String(item.id)] = item;
        }
        for (const item of payload?.flows || []) {
          if (item?.id) f[String(item.id)] = item;
        }
        setZoneActivityById(z);
        setFlowActivityById(f);
      } catch {
        if (!cancelled) {
          setZoneActivityById({});
          setFlowActivityById({});
        }
      }
    };
    void load();
    const timer = window.setInterval(() => { void load(); }, 60_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [scope?.lensId]);

  const clearStackSelection = React.useCallback(() => {
    stackSelectionRef.current?.clearTransient?.();
    stackSelectionRef.current = null;
    setStackSelection(null);
  }, []);

  React.useEffect(() => () => {
    stackSelectionRef.current?.clearTransient?.();
  }, []);

  const handleMarkerSelection = React.useCallback((selection: GeoMarkerSelection) => {
    if (selection.kind === 'clear') {
      clearStackSelection();
      setSelectedObject(null);
      return;
    }

    if (selection.kind === 'object') {
      clearStackSelection();
      setSelectedObject(selection.object);
      return;
    }

    clearStackSelection();
    setSelectedObject(null);
    setStackSelection(selection);
  }, [clearStackSelection]);

  useGeoMarkerLayer(map, isReady, styleEpoch, mapObjects, theme, handleMarkerSelection);
  useGeoZoneLayer(map, isReady, styleEpoch, mapObjects, theme);
  useStaticZoneLayer(map, isReady, styleEpoch, filteredStaticZones, theme, zonesEnabled, zoneActivityById, mapObjects);
  useTransportFlowLayer(map, isReady, styleEpoch, filteredTransportFlows, theme, flowsEnabled, flowActivityById);
  const pathLegStats = usePathLegLayer(map, isReady, styleEpoch, pathArtifacts, allObjects, theme, pathLegsEnabled);
  useVesselLayer(map, isReady, styleEpoch, vesselsEnabled, theme);

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
          pathLegsEnabled={pathLegsEnabled}
          pathLegCount={pathLegStats.legCount}
          vesselsEnabled={vesselsEnabled}
          vesselsApiAvailable={vesselsApiAvailable}
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
          onTogglePathLegsEnabled={() => setPathLegsEnabled((v) => !v)}
          onToggleVesselsEnabled={() => setVesselsEnabled((v) => !v)}
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
          {pathLegsEnabled ? ` | Path legs: ${pathLegStats.legCount}` : ''}
          {vesselsEnabled ? ' | Vessels (AIS)' : ''}
        </div>

        {stackSelection && (
          <MapObjectStackPanel
            objects={stackSelection.objects}
            isDark={isDark}
            paletteTheme={theme}
            onClose={clearStackSelection}
            onShowFan={stackSelection.canSpiderfy ? stackSelection.spiderfy : undefined}
            onSelectObject={(object) => {
              clearStackSelection();
              setSelectedObject(object);
            }}
          />
        )}

        {selectedObject && !stackSelection && (
          <MapObjectDetail
            object={selectedObject}
            isDark={isDark}
            paletteTheme={theme}
            edgesLoading={edgesLoading}
            edgesCount={edgesCount}
            visibleEdgesCount={visibleEdgesCount}
            traceSummary={traceSummary}
            onClose={() => setSelectedObject(null)}
          />
        )}
      </div>
    </div>
  );
};

export default WorldMapPanel;
