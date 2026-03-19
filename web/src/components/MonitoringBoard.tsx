import React, { useState, useEffect } from 'react';
import { CriticalChainMaterial } from '../lib/criticalchain-api';

interface MonitoringBoardProps {
  materials: CriticalChainMaterial[];
  selectedMaterial: string | null;
  onSelect: (id: string) => void;
}

const STORAGE_KEY = 'cc-watchlist';

export const MonitoringBoard: React.FC<MonitoringBoardProps> = ({ materials, selectedMaterial, onSelect }) => {
  const [watchlist, setWatchlist] = useState<string[]>([]);
  const [showAddMenu, setShowAddMenu] = useState(false);

  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      try {
        setWatchlist(JSON.parse(stored));
      } catch (e) {
        setWatchlist(materials.map(m => m.id));
      }
    } else if (materials.length > 0) {
      setWatchlist(materials.map(m => m.id));
    }
  }, [materials]);

  const saveWatchlist = (newList: string[]) => {
    setWatchlist(newList);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(newList));
  };

  const removeMaterial = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    saveWatchlist(watchlist.filter(wId => wId !== id));
  };

  const addMaterial = (id: string) => {
    if (!watchlist.includes(id)) {
      saveWatchlist([...watchlist, id]);
    }
    setShowAddMenu(false);
  };

  const displayedMaterials = materials.filter(m => watchlist.includes(m.id));
  const availableToAdd = materials.filter(m => !watchlist.includes(m.id));

  // Helper to generate consistent mock data based on id
  const getMockData = (id: string, baseRisk: number) => {
    const seed = id.charCodeAt(0) + id.charCodeAt(id.length - 1);
    const incidents = seed % 5;
    const isUp = (seed % 2) === 0;
    const delta = ((seed % 10) / 10).toFixed(1);
    const sparkline = Array.from({ length: 7 }, (_, i) => {
      const val = baseRisk + Math.sin(seed + i) * 0.5;
      return Math.max(0, Math.min(5, val));
    });
    return { incidents, delta: isUp ? `↑${delta}` : `↓${delta}`, isUp, sparkline };
  };

  const getStatus = (score: number) => {
    if (score >= 4) return { label: 'CRITICAL', color: 'bg-red-500/20 text-red-400 border-red-500/30' };
    if (score >= 3) return { label: 'ALERT', color: 'bg-orange-500/20 text-orange-400 border-orange-500/30' };
    if (score >= 2) return { label: 'WATCH', color: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30' };
    return { label: 'STABLE', color: 'bg-green-500/20 text-green-400 border-green-500/30' };
  };

  return (
    <div className="flex items-center gap-3 p-3 overflow-x-auto border-b border-[#21262D] bg-[#0D1117] hide-scrollbar" style={{ minHeight: '120px' }}>
      {displayedMaterials.map(m => {
        const mock = getMockData(m.id, m.riskScore);
        const status = getStatus(m.riskScore);
        const isSelected = selectedMaterial === m.id;

        // Simple SVG Sparkline
        const maxScore = 5;
        const width = 60;
        const height = 24;
        const points = mock.sparkline.map((val, i) => `${(i / 6) * width},${height - (val / maxScore) * height}`).join(' ');

        return (
          <div
            key={m.id}
            onClick={() => onSelect(m.id)}
            className={`relative flex-shrink-0 w-48 p-3 rounded-lg border cursor-pointer transition-all ${
              isSelected ? 'border-[#4FC3F7] bg-[#161B22]' : 'border-[#30363D] bg-[#0D1117] hover:border-[#8B949E]'
            }`}
          >
            <button
              onClick={(e) => removeMaterial(m.id, e)}
              className="absolute top-2 right-2 text-[#8B949E] hover:text-[#E6EDF3]"
            >
              ×
            </button>
            <div className="flex justify-between items-start mb-2">
              <div className="font-semibold text-[13px] truncate pr-4">{m.name}</div>
              {mock.incidents > 0 && (
                <div className="bg-red-500 text-white text-[10px] font-bold px-1.5 py-0.5 rounded-full leading-none mt-0.5">
                  {mock.incidents}
                </div>
              )}
            </div>
            
            <div className={`inline-block px-1.5 py-0.5 text-[10px] font-bold rounded border ${status.color} mb-2`}>
              {status.label}
            </div>

            <div className="flex items-end justify-between">
              <div>
                <div className="text-xl font-mono leading-none">{m.riskScore.toFixed(1)}<span className="text-xs text-[#8B949E]">/5.0</span></div>
                <div className={`text-[11px] font-mono mt-1 ${mock.isUp ? 'text-red-400' : 'text-green-400'}`}>
                  {mock.delta}
                </div>
              </div>
              <svg width={width} height={height} className="overflow-visible">
                <polyline
                  points={points}
                  fill="none"
                  stroke={mock.isUp ? '#f87171' : '#4ade80'}
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </div>
          </div>
        );
      })}

      <div className="relative flex-shrink-0 flex items-center h-full">
        <button
          onClick={() => setShowAddMenu(!showAddMenu)}
          className="w-10 h-10 rounded-full border border-dashed border-[#30363D] flex items-center justify-center text-[#8B949E] hover:text-[#E6EDF3] hover:border-[#8B949E] transition-colors"
        >
          +
        </button>
        
        {showAddMenu && availableToAdd.length > 0 && (
          <div className="absolute top-12 left-0 w-48 bg-[#161B22] border border-[#30363D] rounded-md shadow-xl z-50 max-h-60 overflow-y-auto">
            {availableToAdd.map(m => (
              <button
                key={m.id}
                onClick={() => addMaterial(m.id)}
                className="w-full text-left px-3 py-2 text-sm text-[#E6EDF3] hover:bg-[#21262D] transition-colors"
              >
                {m.name}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
