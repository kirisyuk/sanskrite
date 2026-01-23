import React from 'react';
import type { FragmentProfile } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface TribeProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Tribe/Clan profile dashboard.
 * Uses the same high-grade layout as MarketProfileDashboard, but for tribe/clan content.
 */
export const TribeProfileDashboard: React.FC<TribeProfileDashboardProps> = ({ profile }) => {
  return <MarketProfileDashboard profile={profile} />;
};
