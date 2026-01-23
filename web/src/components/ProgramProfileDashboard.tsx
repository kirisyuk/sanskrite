import React from 'react';
import type { FragmentProfile } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface ProgramProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Program profile dashboard.
 * Uses the same high-grade layout as MarketProfileDashboard, but for program content.
 */
export const ProgramProfileDashboard: React.FC<ProgramProfileDashboardProps> = ({ profile }) => {
  return <MarketProfileDashboard profile={profile} />;
};
