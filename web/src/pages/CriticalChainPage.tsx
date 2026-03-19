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
} from '../lib/criticalchain-api';
import { CriticalChainMap } from '../components/CriticalChainMap';
import type { MapViewMode } from '../components/CriticalChainMap';
import { CriticalChainGraph as CriticalChainGraphComponent } from '../components/CriticalChainGraph';
import { SignalFusionPanel } from '../components/SignalFusionPanel';
import { GlobalStatusBar } from '../components/GlobalStatusBar';
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

function RiskBadge({ score, size = 'sm', tooltip }: any) {
  const color = getRiskColor(score);
  const dotSize = size === 'sm' ? 8 : 12;
  const textSize = size === 'sm' ? '11px' : '13px';
  const title = tooltip || `Risk score: ${score.toFixed(1)}/5.`;
  return (
    <span className="inline-flex items-center gap-1" title={title} style={{ fontSize: textSize }}>
      <span style={{ width: dotSize, height: dotSize, borderRadius: '50%', backgroundColor: color, display: 'inline-block' }} />
      <span style={{ fontFamily: 'JetBrains Mono, monospace' }}>{score.toFixed(1)}</span>
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
  const [viewMode, setViewMode] = useState<'map'|'graph'>('map');
  const [selectedFeature, setSelectedFeature] = useState<any>(null);

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
      (m) => m.name?.toLowerCase().includes(q) || m.id?.toLowerCase().includes(q)
    );
  }, [materials, materialSearch]);

  return (
    <div className="flex flex-col h-full bg-[#0D1117] text-[#E6EDF3]">
      <header className="flex items-center justify-between gap-4 px-4 py-3 border-b border-[#21262D]">
        <div className="flex items-center gap-4 shrink-0">
          <h1 className="text-lg font-semibold">CriticalChain</h1>
          <div className="flex rounded overflow-hidden bg-[#21262D] p-1">
            <button
              onClick={() => setViewMode('map')}
              className={`px-3 py-1 text-xs rounded transition-colors ${viewMode === 'map' ? 'bg-[#0D1117] text-[#4FC3F7]' : 'text-[#8B949E] hover:text-[#E6EDF3]'}`}
            >
              Map
            </button>
            <button
              onClick={() => setViewMode('graph')}
              className={`px-3 py-1 text-xs rounded transition-colors ${viewMode === 'graph' ? 'bg-[#0D1117] text-[#4FC3F7]' : 'text-[#8B949E] hover:text-[#E6EDF3]'}`}
            >
              Graph
            </button>
          </div>
        </div>
        <input
          type="search"
          placeholder="Material Search"
          value={materialSearch}
          onChange={(e) => setMaterialSearch(e.target.value)}
          className="flex-1 max-w-xs px-3 py-1.5 text-[13px] rounded bg-[#0D1117] border border-[#21262D] text-[#E6EDF3] placeholder-[#484F58] focus:outline-none focus:border-[#4FC3F7]"
        />
        <div className="flex items-center gap-2 shrink-0">
          {user ? (
            <button onClick={() => signOut()} className="px-3 py-1.5 text-[13px] rounded bg-[#21262D] text-[#E6EDF3] hover:bg-[#21262D]/80">
              {user.email?.split('@')[0] ?? 'Account'}
            </button>
          ) : (
            <button onClick={() => signInWithGoogle()} className="px-3 py-1.5 text-[13px] rounded bg-[#4FC3F7] text-[#0D1117] hover:bg-[#4FC3F7]/90 font-medium">
              Login
            </button>
          )}
        </div>
      </header>

      <GlobalStatusBar materials={materials} />

      <div className="flex flex-1 min-h-0">
        <aside className="w-56 border-r border-[#21262D] flex flex-col">
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
                        selectedMaterial === m.id ? 'bg-[#21262D] text-[#4FC3F7]' : 'hover:bg-[#21262D]/50 text-[#E6EDF3]'
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
        </aside>

        {viewMode === 'map' ? (
          <>
            <main className="flex-1 min-w-0 flex flex-col">
              <div className="flex-1 min-h-0 relative">
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
                  onFeatureClick={(feat) => setSelectedFeature(feat)}
                />
              </div>
            </main>
            <aside className="w-80 border-l border-[#21262D] flex flex-col">
              <div className="p-3 border-b border-[#21262D]">
                <h2 className="text-sm font-medium text-[#8B949E]">Drill-down</h2>
              </div>
              <div className="flex-1 min-h-0 overflow-y-auto p-3 text-sm">
                {!selectedFeature ? (
                  <div>
                    <h3 className="font-semibold mb-2">Top Global Alerts</h3>
                    {materials.filter(m => m.riskScore >= 4).slice(0, 5).map(m => (
                      <div key={m.id} className="mb-2 p-2 bg-[#161B22] rounded border border-[#21262D]">
                        <div className="flex justify-between">
                          <span className="font-medium text-[#4FC3F7]">{m.name}</span>
                          <RiskBadge score={m.riskScore} />
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div>
                    <h3 className="font-semibold text-[#4FC3F7] mb-2">{selectedFeature.name || 'Selected'}</h3>
                    <p className="text-[#8B949E] text-xs mb-4">Risk Level: <RiskBadge score={selectedFeature.riskScore || 0} /></p>
                    <button className="w-full py-1.5 bg-[#21262D] hover:bg-[#30363D] rounded border border-[#30363D]">
                      Follow Incident
                    </button>
                  </div>
                )}
              </div>
            </aside>
          </>
        ) : (
          <main className="flex-1 min-w-0 flex flex-col bg-[#0D1117]">
            <CriticalChainGraphComponent graph={graph} />
          </main>
        )}
      </div>
    </div>
  );
};

export default CriticalChainPage;
