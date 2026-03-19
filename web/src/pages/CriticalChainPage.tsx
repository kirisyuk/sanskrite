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
import { MonitoringBoard } from '../components/MonitoringBoard';
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

const MaterialGroup = ({ title, items, defaultOpen, selectedMaterial, onSelect }: any) => {
  const [isOpen, setIsOpen] = useState(defaultOpen);

  if (items.length === 0) return null;

  return (
    <div className="mb-2">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between px-2 py-1 text-[11px] font-semibold text-[#8B949E] uppercase tracking-wider hover:text-[#E6EDF3] transition-colors"
      >
        <span>{title} ({items.length})</span>
        <span>{isOpen ? '▼' : '▶'}</span>
      </button>
      {isOpen && (
        <ul className="space-y-1 mt-1">
          {items.map((m: any) => (
            <li key={m.id}>
              <button
                onClick={() => onSelect(m.id)}
                className={`w-full text-left px-3 py-1.5 rounded flex items-center justify-between text-[13px] transition-colors ${
                  selectedMaterial === m.id ? 'bg-[#21262D] text-[#4FC3F7]' : 'hover:bg-[#21262D]/50 text-[#E6EDF3]'
                }`}
              >
                <span className="truncate pr-2">{m.name}</span>
                <RiskBadge score={m.riskScore} size="sm" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

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

  const groupedMaterials = useMemo(() => {
    const groups: Record<string, typeof materials> = { critical: [], alert: [], watch: [], stable: [] };
    filteredMaterials.forEach(m => {
      if (m.riskScore >= 4) groups.critical.push(m);
      else if (m.riskScore >= 3) groups.alert.push(m);
      else if (m.riskScore >= 2) groups.watch.push(m);
      else groups.stable.push(m);
    });
    // Sort descending by risk score
    Object.values(groups).forEach(g => g.sort((a, b) => b.riskScore - a.riskScore));
    return groups;
  }, [filteredMaterials]);

  const handleShare = () => {
    const url = new URL(window.location.href);
    if (selectedMaterial) url.searchParams.set('material', selectedMaterial);
    // Dummy values based on instructions
    url.searchParams.set('region', 'south-america');
    url.searchParams.set('zoom', '4');
    
    navigator.clipboard.writeText(url.toString()).then(() => {
      // Basic toast
      const toast = document.createElement('div');
      toast.className = 'fixed bottom-4 right-4 bg-green-500 text-white px-4 py-2 rounded shadow-lg z-50 text-sm font-medium';
      toast.innerText = 'Link copied';
      document.body.appendChild(toast);
      setTimeout(() => toast.remove(), 2500);
    });
  };

  const handleExport = () => {
    window.print();
  };

  return (
    <div className="flex flex-col h-screen bg-[#0D1117] text-[#E6EDF3] print-container">
      <style>
        {`
          @media print {
            body { background: white !important; color: black !important; }
            .print-container { height: auto !important; display: block !important; }
            .no-print { display: none !important; }
            .print-only { display: block !important; }
            .print-board { overflow: visible !important; flex-wrap: wrap !important; border: none !important; }
            * { text-shadow: none !important; box-shadow: none !important; }
            .drill-down { border: 1px solid #ccc !important; padding: 15px !important; margin-top: 20px !important; width: 100% !important; page-break-inside: avoid; }
            /* Hide scrolling parts */
            .hide-scrollbar::-webkit-scrollbar { display: none; }
          }
          .print-only { display: none; }
        `}
      </style>

      <div className="print-only text-xs text-gray-500 mb-4 pb-2 border-b">
        Exported on {new Date().toLocaleString()}
      </div>

      <header className="flex items-center justify-between gap-4 px-4 py-3 border-b border-[#21262D] no-print">
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
        <div className="flex-1 flex justify-center max-w-xs mx-auto">
          <input
            type="search"
            placeholder="Material Search"
            value={materialSearch}
            onChange={(e) => setMaterialSearch(e.target.value)}
            className="w-full px-3 py-1.5 text-[13px] rounded bg-[#0D1117] border border-[#21262D] text-[#E6EDF3] placeholder-[#484F58] focus:outline-none focus:border-[#4FC3F7]"
          />
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button onClick={handleShare} className="px-3 py-1.5 text-[13px] rounded border border-[#30363D] text-[#E6EDF3] hover:bg-[#21262D]">
            Share
          </button>
          <button onClick={handleExport} className="px-3 py-1.5 text-[13px] rounded border border-[#30363D] text-[#E6EDF3] hover:bg-[#21262D]">
            Export
          </button>
          {user ? (
            <button onClick={() => signOut()} className="px-3 py-1.5 text-[13px] rounded bg-[#21262D] text-[#E6EDF3] hover:bg-[#21262D]/80 ml-2">
              {user.email?.split('@')[0] ?? 'Account'}
            </button>
          ) : (
            <button onClick={() => signInWithGoogle()} className="px-3 py-1.5 text-[13px] rounded bg-[#4FC3F7] text-[#0D1117] hover:bg-[#4FC3F7]/90 font-medium ml-2">
              Login
            </button>
          )}
        </div>
      </header>

      <div className="print-board">
        <MonitoringBoard 
          materials={materials} 
          selectedMaterial={selectedMaterial} 
          onSelect={setSelectedMaterial} 
        />
      </div>

      <div className="flex flex-1 min-h-0 no-print">
        <aside className="w-56 border-r border-[#21262D] flex flex-col">
          <div className="flex-1 overflow-y-auto p-2">
            {materialsLoading ? (
              <div className="text-sm text-[#8B949E]">Loading...</div>
            ) : (
              <div className="space-y-2">
                <MaterialGroup title="Critical" items={groupedMaterials.critical} defaultOpen={true} selectedMaterial={selectedMaterial} onSelect={setSelectedMaterial} />
                <MaterialGroup title="Alert" items={groupedMaterials.alert} defaultOpen={true} selectedMaterial={selectedMaterial} onSelect={setSelectedMaterial} />
                <MaterialGroup title="Watch" items={groupedMaterials.watch} defaultOpen={false} selectedMaterial={selectedMaterial} onSelect={setSelectedMaterial} />
                <MaterialGroup title="Stable" items={groupedMaterials.stable} defaultOpen={false} selectedMaterial={selectedMaterial} onSelect={setSelectedMaterial} />
              </div>
            )}
          </div>
        </aside>

        {viewMode === 'map' ? (
          <>
            <main className="flex-1 min-w-0 flex flex-col">
              {(facilities ?? []).length === 0 && (routes ?? []).length === 0 && (
                <div className="shrink-0 px-4 py-2 bg-[#161B22] border-b border-[#21262D] text-[#8B949E] text-xs">
                  No facilities or routes. Run the pipeline to load data from USGS, EIA, ENTSOG.
                </div>
              )}
              <div className="flex-1 min-h-0 relative">
                <CriticalChainMap
                  facilities={facilities ?? []}
                  routes={routes ?? []}
                  conflictZones={conflictZones ?? []}
                  chokepoints={chokepoints ?? []}
                  countryRisk={countryRisk ?? {}}
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
            <aside className="w-80 border-l border-[#21262D] flex flex-col drill-down">
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
                  <div className="mb-6">
                    <h3 className="font-semibold text-[#4FC3F7] mb-2">{selectedFeature.name || 'Selected'}</h3>
                    <p className="text-[#8B949E] text-xs mb-4">Risk Level: <RiskBadge score={selectedFeature.riskScore || 0} /></p>
                    <button className="w-full py-1.5 bg-[#21262D] hover:bg-[#30363D] rounded border border-[#30363D] no-print">
                      Follow Incident
                    </button>
                  </div>
                )}
                
                <div className="mt-6 pt-4 border-t border-[#21262D]">
                  <SignalFusionPanel />
                </div>
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
