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

  const getStatus = (score: number) => {
    if (score >= 4) return { label: 'CRITICAL', color: 'bg-red-500/20 text-red-400 border-red-500/30' };
    if (score >= 3) return { label: 'ALERT', color: 'bg-orange-500/20 text-orange-400 border-orange-500/30' };
    if (score >= 2) return { label: 'WATCH', color: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30' };
    return { label: 'STABLE', color: 'bg-green-500/20 text-green-400 border-green-500/30' };
  };

  return (
    <div className="flex items-center gap-3 p-3 overflow-x-auto border-b border-[#21262D] bg-[#0D1117] hide-scrollbar" style={{ minHeight: '120px' }}>
      {displayedMaterials.map(m => {
        const status = getStatus(m.riskScore);
        const isSelected = selectedMaterial === m.id;

        const historyPoints = m.riskHistory && m.riskHistory.length > 0
          ? m.riskHistory.slice(-7).map(pt => pt.value)
          : [];
        const hasHistory = historyPoints.length > 0;
        
        const delta = m.riskDelta7d || 0;
        const isUp = delta > 0;
        const deltaStr = delta !== 0 ? `${delta > 0 ? '↑' : '↓'}${Math.abs(delta).toFixed(1)}` : null;

        const maxScore = 5;
        const width = 60;
        const height = 24;
        
        let points = '';
        if (hasHistory) {
          const step = historyPoints.length > 1 ? width / (historyPoints.length - 1) : width;
          points = historyPoints.map((val, i) => `${i * step},${height - (val / maxScore) * height}`).join(' ');
        }

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
            </div>
            
            <div className={`inline-block px-1.5 py-0.5 text-[10px] font-bold rounded border ${status.color} mb-2`}>
              {status.label}
            </div>

            <div className="flex items-end justify-between">
              <div>
                <div className="text-xl font-mono leading-none">{m.riskScore.toFixed(1)}<span className="text-xs text-[#8B949E]">/5.0</span></div>
                {deltaStr && (
                  <div className={`text-[11px] font-mono mt-1 ${isUp ? 'text-red-400' : 'text-green-400'}`}>
                    {deltaStr}
                  </div>
                )}
              </div>
              {hasHistory ? (
                <svg width={width} height={height} className="overflow-visible">
                  <polyline
                    points={points}
                    fill="none"
                    stroke={isUp ? '#f87171' : '#4ade80'}
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              ) : (
                <div className="text-[10px] text-[#8B949E] flex items-center h-[24px]">no history yet</div>
              )}
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
