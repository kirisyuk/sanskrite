import React from 'react';
import type { FragmentProfile } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface DynastyProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Dynasty/Family profile dashboard.
 * Uses the same high-grade layout as MarketProfileDashboard, but for dynasty/family content.
 */
export const DynastyProfileDashboard: React.FC<DynastyProfileDashboardProps> = ({ profile }) => {
  return <MarketProfileDashboard profile={profile} />;
};
