import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { GovProfileDashboard } from './GovProfileDashboard';

export interface GovProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Government-specific profile modal wrapper.
 * Semantically these are ORGANIZATION fragments under /gov/*, but the UI visuals are gov-specific.
 */
export const GovProfileModal: React.FC<GovProfileModalProps> = ({
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
      fragmentType="ORGANIZATION"
      canonicalPath={canonicalPath}
      label={label}
    >
      {(profile, loading, error) => {
        if (loading) return null;
        if (error) return null;
        return <GovProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};

