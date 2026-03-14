import { useState, useEffect, useMemo, useRef } from 'react';
import { api } from '../lib/api';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';

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
  zoneType?: string;
  zoneSubType?: string;
  hype?: number;
  hypeIndicator?: number;
  activityStatus?: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
  lastMatchedAt?: string;
  inScope?: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type TypeCount = { type: string; count: number };

export interface GeoObjectFilters {
  dateFrom: string;
  dateTo: string;
  activeTypes: Set<string> | null;
}

const GEO_OBJECT_LIMIT_CANDIDATES = [5000] as const;

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
  canonicalPathsSource?: string[] | null,
  preferCanonicalPaths: boolean = false,
) {
  const [allObjects, setAllObjects] = useState<GeoObject[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const horizonEvents = useHorizonRealtimeStore((s) => s.events);

  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const lastEventIdRef = useRef<string | null>(null);
  const realtimeInitializedRef = useRef(false);

  useEffect(() => {
    let cancelled = false;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      setLoading(true);

      const load = async () => {
        let lastError: unknown = null;
        const canonicalPaths = Array.isArray(canonicalPathsSource)
          ? Array.from(new Set(canonicalPathsSource.map((value) => String(value || '').trim()).filter(Boolean)))
          : [];

        // Keep the lens world map populated while supply-path artifacts are still loading.
        // Only switch to the by-path endpoint when we have an actual non-empty path set.
        if (scope?.lensId && preferCanonicalPaths && canonicalPaths.length > 0) {
            try {
              const response = await api.listHorizonGeoObjectsByPaths({
                canonicalPaths,
                createdAfter: filters.dateFrom || undefined,
                createdBefore: filters.dateTo || undefined,
              });
              if (cancelled) return;
              const valid = (response.items || []).filter((item) => {
                const lat = item.coordinates?.lat;
                const lon = item.coordinates?.lon;
                return Number.isFinite(lat) && Number.isFinite(lon);
              });
              setAllObjects(valid);
              setError(null);
              return;
            } catch (err) {
              lastError = err;
          }
        }
        for (const limit of GEO_OBJECT_LIMIT_CANDIDATES) {
          const opts: Parameters<typeof api.listHorizonGeoObjects>[0] = { limit };
          if (filters.dateFrom) opts.createdAfter = filters.dateFrom;
          if (filters.dateTo) opts.createdBefore = filters.dateTo;
          if (scope?.lensId) opts.lensId = scope.lensId;

          try {
            const response = await api.listHorizonGeoObjects(opts);
            if (cancelled) return;
            const valid = (response.items || []).filter((item) => {
              const lat = item.coordinates?.lat;
              const lon = item.coordinates?.lon;
              return Number.isFinite(lat) && Number.isFinite(lon);
            });
            setAllObjects(valid);
            setError(null);
            return;
          } catch (err) {
            lastError = err;
            const status = Number((err as any)?.response?.status);
            const code = String((err as any)?.code || '');
            const shouldRetry = status === 503 || code === 'ERR_NETWORK';
            if (!shouldRetry) break;
          }
        }

        if (!cancelled) {
          setError(lastError instanceof Error ? lastError.message : 'Failed to load geo objects');
        }
      };

      void load().finally(() => {
        if (!cancelled) setLoading(false);
      });
    }, 300);

    return () => {
      cancelled = true;
      clearTimeout(debounceRef.current);
    };
  }, [canonicalPathsSource, filters.dateFrom, filters.dateTo, preferCanonicalPaths, scope?.lensId]);

  useEffect(() => {
    if (!Array.isArray(horizonEvents) || horizonEvents.length === 0) return;
    const latest = horizonEvents[horizonEvents.length - 1];
    if (!latest?.id) return;

    if (!realtimeInitializedRef.current) {
      realtimeInitializedRef.current = true;
      lastEventIdRef.current = latest.id;
      return;
    }

    let fromIdx = horizonEvents.findIndex((ev) => ev?.id === lastEventIdRef.current);
    if (fromIdx < 0) fromIdx = horizonEvents.length - 1;
    const updates = horizonEvents.slice(fromIdx + 1);
    lastEventIdRef.current = latest.id;
    if (updates.length === 0) return;

    setAllObjects((prev) => {
      let next = prev;
      let changed = false;
      for (const ev of updates) {
        if (ev?.type !== 'horizon.graph.node.upserted') continue;
        const data = ev?.data || {};
        const canonicalPath = typeof data?.canonicalPath === 'string' ? data.canonicalPath.trim() : '';
        const rawType = typeof data?.type === 'string' ? data.type.trim().toUpperCase() : '';
        const isAffectedZone = rawType === 'AFFECTED_ZONE' || canonicalPath.startsWith('/zone/');
        if (!isAffectedZone) continue;
        const lat = Number(data?.coordinates?.lat);
        const lon = Number(data?.coordinates?.lon);
        if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
        const id = String(data?.fragmentId || canonicalPath || '').trim();
        if (!id || !canonicalPath) continue;
        const radiusKm = Number(data?.radiusKm);
        const activityStatusRaw = typeof data?.activityStatus === 'string' ? data.activityStatus.trim().toLowerCase() : '';
        const activityStatus =
          activityStatusRaw === 'hot' ||
          activityStatusRaw === 'active' ||
          activityStatusRaw === 'watch' ||
          activityStatusRaw === 'cooling' ||
          activityStatusRaw === 'inactive'
            ? activityStatusRaw
            : undefined;
        const nextObj: GeoObject = {
          id,
          canonicalPath,
          type: 'AFFECTED_ZONE',
          label: typeof data?.label === 'string' ? data.label.trim() : undefined,
          status: typeof data?.status === 'string' ? data.status.trim() : undefined,
          coordinates: { lat, lon },
          radiusKm: Number.isFinite(radiusKm) ? radiusKm : undefined,
          zoneType: typeof data?.zoneType === 'string' ? data.zoneType.trim() : undefined,
          zoneSubType: typeof data?.zoneSubType === 'string' ? data.zoneSubType.trim() : undefined,
          hype: Number.isFinite(Number(data?.hype)) ? Number(data?.hype) : undefined,
          hypeIndicator: Number.isFinite(Number(data?.hypeIndicator)) ? Number(data?.hypeIndicator) : undefined,
          activityStatus,
          lastMatchedAt: typeof data?.lastMatchedAt === 'string' ? data.lastMatchedAt : undefined,
          updatedAt: typeof data?.updatedAt === 'string' ? data.updatedAt : new Date().toISOString(),
        };
        const idx = next.findIndex((obj) => obj.id === id || obj.canonicalPath === canonicalPath);
        if (idx >= 0) {
          const merged = { ...next[idx], ...nextObj };
          const same = JSON.stringify(next[idx]) === JSON.stringify(merged);
          if (!same) {
            if (!changed) next = [...next];
            next[idx] = merged;
            changed = true;
          }
        } else {
          if (!changed) next = [...next];
          next.unshift(nextObj);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [horizonEvents]);

  const typeCounts = useMemo(() => deriveTypeCounts(allObjects), [allObjects]);

  const filteredObjects = useMemo(() => {
    if (!filters.activeTypes) return allObjects;
    return allObjects.filter((obj) => filters.activeTypes!.has(obj.type));
  }, [allObjects, filters.activeTypes]);

  return { allObjects, filteredObjects, typeCounts, loading, error };
}
