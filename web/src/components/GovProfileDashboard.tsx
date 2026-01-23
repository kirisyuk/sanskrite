import React from 'react';
import type { FragmentProfile } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface GovProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Government profile dashboard.
 * Uses the same high-grade layout as MarketProfileDashboard, but for /gov content.
 */
export const GovProfileDashboard: React.FC<GovProfileDashboardProps> = ({ profile }) => {
  return <MarketProfileDashboard profile={profile} />;
};

