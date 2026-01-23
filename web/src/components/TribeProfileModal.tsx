import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { TribeProfileDashboard } from './TribeProfileDashboard';

export interface TribeProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Tribe/Clan-specific profile modal wrapper.
 * These are COMMUNITY fragments with communityType="tribe" or path /community/{geo}/tribe/{slug}.
 */
export const TribeProfileModal: React.FC<TribeProfileModalProps> = ({
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
      fragmentType="COMMUNITY"
      canonicalPath={canonicalPath}
      label={label}
    >
      {(profile, loading, error) => {
        if (loading) return null;
        if (error) return null;
        return <TribeProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};
