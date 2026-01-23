import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { DynastyProfileDashboard } from './DynastyProfileDashboard';

export interface DynastyProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Dynasty/Family-specific profile modal wrapper.
 * These are COMMUNITY fragments with communityType="dynasty" or path /community/{geo}/dynasty/{slug}.
 */
export const DynastyProfileModal: React.FC<DynastyProfileModalProps> = ({
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
        return <DynastyProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};
