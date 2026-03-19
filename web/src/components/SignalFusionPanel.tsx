import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchSignals, type CriticalChainFusedSignal } from '../lib/criticalchain-api';

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

export const SignalFusionPanel: React.FC = () => {
  const { data, isLoading } = useQuery({
    queryKey: ['criticalchain', 'signals'],
    queryFn: async () => {
      const res = await fetchSignals();
      return res.regions || [];
    },
  });

  const signals: CriticalChainFusedSignal[] = data ?? [];

  return (
    <div className="flex flex-col">
      <h3 className="text-xs font-medium text-[#8B949E] mb-2">Signal fusion</h3>
      {isLoading ? (
        <div className="text-[11px] text-[#484F58]">Loading...</div>
      ) : signals.length === 0 ? (
        <div className="text-[11px] text-[#484F58]">
          No regional signals. Data refreshes every 6h.
        </div>
      ) : (
        <ul className="space-y-2">
          {signals.map((s) => (
            <li
              key={s.region}
              className="flex items-center justify-between text-[11px] py-1 px-2 rounded"
              style={{ backgroundColor: 'rgba(33,38,45,0.6)' }}
            >
              <span className="text-[#E6EDF3] truncate" title={s.region}>
                {s.region}
              </span>
              <span
                className="inline-flex items-center gap-1 font-mono"
                style={{ color: getRiskColor(s.risk_level) }}
              >
                <span
                  style={{
                    width: 6,
                    height: 6,
                    borderRadius: '50%',
                    backgroundColor: getRiskColor(s.risk_level),
                  }}
                />
                {s.risk_level.toFixed(1)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
