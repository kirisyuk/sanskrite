import React from 'react';
import { useMapInstance } from '../hooks/useMapInstance';
import { useGeoObjects, type GeoObject } from '../hooks/useGeoObjects';
import { useGeoMarkerLayer } from '../hooks/useGeoMarkerLayer';
import { useGeoZoneLayer } from '../hooks/useGeoZoneLayer';
import { useGeoEdgeLayer } from '../hooks/useGeoEdgeLayer';
import { MapFilterPanel } from './MapFilterPanel';
import { MapObjectDetail } from './MapObjectDetail';

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
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);

  const filters = React.useMemo(
    () => ({ dateFrom, dateTo, activeTypes }),
    [dateFrom, dateTo, activeTypes],
  );

  const { allObjects, filteredObjects, typeCounts, loading, error } = useGeoObjects(scope, filters);

  useGeoMarkerLayer(map, isReady, styleEpoch, filteredObjects, theme, setSelectedObject);
  useGeoZoneLayer(map, isReady, styleEpoch, filteredObjects, theme);
  const { edgesLoading, edgesCount } = useGeoEdgeLayer(map, isReady, styleEpoch, selectedObject, filteredObjects);

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
          activeTypes={activeTypes}
          dateFrom={dateFrom}
          dateTo={dateTo}
          loadingData={loading}
          onSetDateFrom={setDateFrom}
          onSetDateTo={setDateTo}
          onToggleType={handleToggleType}
          onSelectAll={() => setActiveTypes(null)}
          onSelectNone={() => setActiveTypes(new Set())}
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
