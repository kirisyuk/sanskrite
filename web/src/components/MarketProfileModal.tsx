import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { MarketProfileDashboard } from './MarketProfileDashboard';

export interface MarketProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Market-specific profile modal wrapper.
 * Uses FragmentProfileModal with MarketProfileDashboard for MARKET fragment types.
 */
export const MarketProfileModal: React.FC<MarketProfileModalProps> = ({
  open,
  onClose,
  fragmentId,
  canonicalPath,
  label,
}) => {
  return (
    <FragmentProfileModal
      open={open}
      onClose={onClose}
      fragmentId={fragmentId}
      fragmentType="MARKET"
      canonicalPath={canonicalPath}
      label={label}
    >
      {(profile, loading, error) => {
        if (loading) {
          return null; // Loading handled by parent modal
        }
        if (error) {
          return null; // Error handled by parent modal
        }
        return <MarketProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};
