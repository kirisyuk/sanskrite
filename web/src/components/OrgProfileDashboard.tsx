import React from 'react';
import type { FragmentProfile } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface OrgProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Organization profile dashboard.
 * Uses the same high-grade layout as MarketProfileDashboard, but for /org content.
 */
export const OrgProfileDashboard: React.FC<OrgProfileDashboardProps> = ({ profile }) => {
  return <MarketProfileDashboard profile={profile} />;
};

