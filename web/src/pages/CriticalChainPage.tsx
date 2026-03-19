import React, { useState, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  fetchMaterials,
  fetchFacilities,
  fetchRoutes,
  fetchConflictZones,
  fetchCountryRisk,
  fetchChokepoints,
  fetchGraph,
  type CriticalChainFacility,
  type CriticalChainRoute,
  type CriticalChainConflictZone,
} from '../lib/criticalchain-api';
import { CriticalChainMap } from '../components/CriticalChainMap';
import type { MapViewMode } from '../components/CriticalChainMap';
import { CriticalChainGraph as CriticalChainGraphComponent } from '../components/CriticalChainGraph';
import { SignalFusionPanel } from '../components/SignalFusionPanel';
import { useAuth } from '../contexts/AuthContext';

const RISK_COLORS: Record<number, string> = {
  1: '#00C853',
  2: '#00C853',
  3: '#FFD700',
  4: '#FF8C00',
  5: '#FF4444',
};

function getRiskColor(score: number): string {
  const level = Math.min(5, Math.max(1, Math.round(score)));
  return RISK_COLORS[level] ?? '#484F58';
}

function RiskBadge({
  score,
  size = 'sm',
  tooltip,
}: {
  score: number;
  size?: 'sm' | 'md';
  tooltip?: string;
}) {
  const color = getRiskColor(score);
  const dotSize = size === 'sm' ? 8 : 12;
  const textSize = size === 'sm' ? '11px' : '13px';
  const title =
    tooltip ||
    `Risk score: ${score.toFixed(1)}/5. ⚠ AI-generated. Experimental.`;
  return (
    <span
      className="inline-flex items-center gap-1"
      title={title}
      style={{ fontSize: textSize }}
    >
      <span
        style={{
          width: dotSize,
          height: dotSize,
          borderRadius: '50%',
          backgroundColor: color,
          display: 'inline-block',
        }}
      />
      <span style={{ fontFamily: 'JetBrains Mono, monospace' }}>
        {score.toFixed(1)}
      </span>
    </span>
  );
}

const CriticalChainPage: React.FC = () => {
  const { user, signOut, signInWithGoogle } = useAuth();
  const [selectedMaterial, setSelectedMaterial] = useState<string | null>(null);
  const [filterRegion, setFilterRegion] = useState('');
  const [filterRiskMin, setFilterRiskMin] = useState<number | ''>('');
  const [mapViewMode, setMapViewMode] = useState<MapViewMode>('overview');
  const [materialSearch, setMaterialSearch] = useState('');

  const { data: materials = [], isLoading: materialsLoading } = useQuery({
    queryKey: ['criticalchain', 'materials'],
    queryFn: fetchMaterials,
  });

  const facilityFilters = {
    material: selectedMaterial ?? undefined,
    region: filterRegion || undefined,
    riskMin: filterRiskMin !== '' ? filterRiskMin : undefined,
  };

  const { data: facilities = [] } = useQuery({
    queryKey: ['criticalchain', 'facilities', facilityFilters],
    queryFn: () => fetchFacilities(facilityFilters),
  });

  const { data: routes = [] } = useQuery({
    queryKey: ['criticalchain', 'routes'],
    queryFn: fetchRoutes,
  });

  const { data: conflictZones = [] } = useQuery({
    queryKey: ['criticalchain', 'conflict-zones'],
    queryFn: fetchConflictZones,
  });

  const { data: countryRisk = {} } = useQuery({
    queryKey: ['criticalchain', 'country-risk'],
    queryFn: fetchCountryRisk,
  });

  const { data: chokepoints = [] } = useQuery({
    queryKey: ['criticalchain', 'chokepoints', selectedMaterial],
    queryFn: () => fetchChokepoints(selectedMaterial ?? undefined),
  });

  const { data: graph = null } = useQuery({
    queryKey: ['criticalchain', 'graph', selectedMaterial],
    queryFn: () => fetchGraph(selectedMaterial!),
    enabled: !!selectedMaterial,
  });

  const filteredMaterials = useMemo(() => {
    if (!materialSearch.trim()) return materials;
    const q = materialSearch.toLowerCase().trim();
    return materials.filter(
      (m) =>
        m.name?.toLowerCase().includes(q) ||
        m.id?.toLowerCase().includes(q)
    );
  }, [materials, materialSearch]);

  return (
    <div className="flex flex-col h-full bg-[#0D1117] text-[#E6EDF3]">
      <header className="flex items-center justify-between gap-4 px-4 py-3 border-b border-[#21262D]">
        <h1 className="text-lg font-semibold shrink-0">CriticalChain</h1>
        <input
          type="search"
          placeholder="Material Search"
          value={materialSearch}
          onChange={(e) => setMaterialSearch(e.target.value)}
          className="flex-1 max-w-xs px-3 py-1.5 text-[13px] rounded bg-[#0D1117] border border-[#21262D] text-[#E6EDF3] placeholder-[#484F58] focus:outline-none focus:border-[#4FC3F7]"
        />
        <div className="flex items-center gap-2 shrink-0">
          <button
            onClick={() => user && undefined}
            disabled={!user}
            title={user ? 'Alerts (coming soon)' : 'Available after login'}
            className={`px-3 py-1.5 text-[13px] rounded ${
              user
                ? 'bg-[#21262D] text-[#E6EDF3] hover:bg-[#21262D]/80'
                : 'bg-[#21262D]/50 text-[#484F58] cursor-not-allowed'
            }`}
          >
            Alerts
          </button>
          {user ? (
            <button
              onClick={() => signOut()}
              className="px-3 py-1.5 text-[13px] rounded bg-[#21262D] text-[#E6EDF3] hover:bg-[#21262D]/80"
              title={user.email ?? 'Sign out'}
            >
              {user.email?.split('@')[0] ?? 'Account'}
            </button>
          ) : (
            <button
              onClick={() => signInWithGoogle()}
              className="px-3 py-1.5 text-[13px] rounded bg-[#4FC3F7] text-[#0D1117] hover:bg-[#4FC3F7]/90 font-medium"
            >
              Login
            </button>
          )}
        </div>
      </header>

      <div className="flex flex-1 min-h-0">
        {/* Left panel: materials */}
        <aside className="w-56 border-r border-[#21262D] flex flex-col">
          <div className="p-3 border-b border-[#21262D]">
            <h2 className="text-sm font-medium text-[#8B949E]">Materials</h2>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            {materialsLoading ? (
              <div className="text-sm text-[#8B949E]">Loading...</div>
            ) : (
              <ul className="space-y-1">
                {filteredMaterials.map((m) => (
                  <li key={m.id}>
                    <button
                      onClick={() => setSelectedMaterial(m.id)}
                      className={`w-full text-left px-3 py-2 rounded flex items-center justify-between text-sm transition-colors ${
                        selectedMaterial === m.id
                          ? 'bg-[#21262D] text-[#4FC3F7]'
                          : 'hover:bg-[#21262D]/50 text-[#E6EDF3]'
                      }`}
                    >
                      <span>{m.name}</span>
                      <RiskBadge score={m.riskScore} size="sm" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="p-2 border-b border-[#21262D]">
            <h3 className="text-xs font-medium text-[#8B949E] mb-2">Filters</h3>
            <div className="space-y-2">
              <input
                type="text"
                placeholder="Region (ISO3)"
                value={filterRegion}
                onChange={(e) => setFilterRegion(e.target.value)}
                className="w-full px-2 py-1 text-[11px] rounded bg-[#0D1117] border border-[#21262D] text-[#E6EDF3] placeholder-[#484F58]"
              />
              <select
                value={filterRiskMin === '' ? '' : String(filterRiskMin)}
                onChange={(e) =>
                  setFilterRiskMin(e.target.value === '' ? '' : Number(e.target.value))
                }
                className="w-full px-2 py-1 text-[11px] rounded bg-[#0D1117] border border-[#21262D] text-[#E6EDF3]"
              >
                <option value="">Risk: any</option>
                <option value="3">Risk ≥ 3</option>
                <option value="4">Risk ≥ 4</option>
                <option value="5">Risk ≥ 5</option>
              </select>
            </div>
          </div>
          <div className="p-2 border-t border-[#21262D]">
            <p className="text-[11px] text-[#484F58]">
              Risk scores: 1 (low) – 5 (critical)
            </p>
          </div>
        </aside>

        {/* Center: map — whitepaper 4.4 */}
        <main className="flex-1 min-w-0 flex flex-col">
          <div className="flex items-center gap-2 px-2 py-1 border-b border-[#21262D]">
            <span className="text-xs text-[#8B949E]">Map:</span>
            {(['overview', 'flow', 'node'] as const).map((mode) => (
              <button
                key={mode}
                onClick={() => setMapViewMode(mode)}
                className={`px-2 py-1 text-[11px] rounded capitalize ${
                  mapViewMode === mode
                    ? 'bg-[#21262D] text-[#4FC3F7]'
                    : 'text-[#8B949E] hover:bg-[#21262D]/50'
                }`}
              >
                {mode}
              </button>
            ))}
          </div>
          <div className="flex-1 min-h-0">
            <CriticalChainMap
              facilities={facilities}
              routes={routes}
              conflictZones={conflictZones}
              chokepoints={chokepoints}
              countryRisk={countryRisk}
              selectedMaterial={selectedMaterial}
              viewMode={mapViewMode}
              layerChoropleth
              layerChokepoints
              layerFacilities
              layerRoutes
              layerConflictZones
            />
          </div>
        </main>

        {/* Right panel: graph */}
        <aside className="w-80 border-l border-[#21262D] flex flex-col">
          <div className="p-3 border-b border-[#21262D]">
            <h2 className="text-sm font-medium text-[#8B949E]">Chain graph</h2>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto">
            <CriticalChainGraphComponent graph={graph} />
          </div>
          <div className="p-2 border-t border-[#21262D]">
            <SignalFusionPanel />
          </div>
          <p className="p-2 text-[11px] text-[#484F58]">
            ⚠ AI-оценка. Экспериментально.
          </p>
        </aside>
      </div>

      <div className="px-4 py-2 border-t border-[#21262D] text-[11px] text-[#484F58]">
        Risk estimates are generated automatically from open sources using AI. They are informational only and not intended as recommendations. Do not use them as the sole basis for commercial, investment, or operational decisions.
      </div>
    </div>
  );
};

export default CriticalChainPage;
