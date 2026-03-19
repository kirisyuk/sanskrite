import React from 'react';

export function GlobalStatusBar({ materials }: { materials: any[] }) {
  const activeEvents = materials.reduce((acc, m) => acc + (m.riskScore >= 4 ? 1 : 0), 0);
  
  const getRiskLabel = (score: number) => {
    if (score >= 5) return { label: 'CRITICAL', color: '#EF4444' };
    if (score >= 4) return { label: 'ALERT', color: '#F97316' };
    if (score >= 3) return { label: 'WATCH', color: '#EAB308' };
    return { label: 'STABLE', color: '#22C55E' };
  };

  const topMaterials = materials.slice(0, 5);

  return (
    <div className="flex items-center gap-4 px-4 py-2 bg-[#161B22] border-b border-[#21262D] text-xs">
      <span className="font-medium text-[#E6EDF3]">[!] {activeEvents} активных событий</span>
      <div className="h-4 w-px bg-[#30363D]" />
      <div className="flex gap-4">
        {topMaterials.map(m => {
          const { label, color } = getRiskLabel(m.riskScore);
          return (
            <span key={m.id}>
              <span className="text-[#8B949E]">{m.name}: </span>
              <span style={{ color }}>{label}</span>
            </span>
          );
        })}
      </div>
    </div>
  );
}
