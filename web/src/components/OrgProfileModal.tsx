import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { OrgProfileDashboard } from './OrgProfileDashboard';

export interface OrgProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Organization-specific profile modal wrapper.
 * Uses FragmentProfileModal with OrgProfileDashboard for ORGANIZATION fragments.
 */
export const OrgProfileModal: React.FC<OrgProfileModalProps> = ({
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
        return <OrgProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};

