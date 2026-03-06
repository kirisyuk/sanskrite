import { useState, useEffect, useMemo, useRef } from 'react';
import { api } from '../lib/api';

export type GeoObject = {
  id: string;
  canonicalPath: string;
  type: string;
  facilityKind?: string;
  label?: string;
  description?: string;
  status?: string;
  coordinates?: { lat: number; lon: number };
  radiusKm?: number;
  inScope?: boolean;
  createdAt?: string;
};

export type TypeCount = { type: string; count: number };

export interface GeoObjectFilters {
  dateFrom: string;
  dateTo: string;
  activeTypes: Set<string> | null;
}

function deriveTypeCounts(objects: GeoObject[]): TypeCount[] {
  const counts = new Map<string, number>();
  for (const obj of objects) {
    counts.set(obj.type, (counts.get(obj.type) || 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
}

export function useGeoObjects(
  scope: { lensId: string } | undefined,
  filters: GeoObjectFilters,
) {
  const [allObjects, setAllObjects] = useState<GeoObject[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      let cancelled = false;
      setLoading(true);

      const opts: Parameters<typeof api.listHorizonGeoObjects>[0] = { limit: 5000 };
      if (filters.dateFrom) opts!.createdAfter = filters.dateFrom;
      if (filters.dateTo) opts!.createdBefore = filters.dateTo;
      if (scope?.lensId) opts!.lensId = scope.lensId;

      api
        .listHorizonGeoObjects(opts)
        .then((response) => {
          if (cancelled) return;
          const valid = (response.items || []).filter((item) => {
            const lat = item.coordinates?.lat;
            const lon = item.coordinates?.lon;
            return Number.isFinite(lat) && Number.isFinite(lon);
          });
          setAllObjects(valid);
          setError(null);
        })
        .catch((err) => {
          if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load geo objects');
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });

      return () => { cancelled = true; };
    }, 300);

    return () => clearTimeout(debounceRef.current);
  }, [filters.dateFrom, filters.dateTo, scope?.lensId]);

  const typeCounts = useMemo(() => deriveTypeCounts(allObjects), [allObjects]);

  const filteredObjects = useMemo(() => {
    if (!filters.activeTypes) return allObjects;
    return allObjects.filter((obj) => filters.activeTypes!.has(obj.type));
  }, [allObjects, filters.activeTypes]);

  return { allObjects, filteredObjects, typeCounts, loading, error };
}
