import React from 'react';
import { FragmentProfileModal } from './FragmentProfileModal';
import { ProgramProfileDashboard } from './ProgramProfileDashboard';

export interface ProgramProfileModalProps {
  open: boolean;
  onClose: () => void;
  fragmentId: string;
  canonicalPath?: string;
  label?: string;
}

/**
 * Program-specific profile modal wrapper.
 * These are PROGRAM fragments (government programs, corporate initiatives, research projects, etc.).
 */
export const ProgramProfileModal: React.FC<ProgramProfileModalProps> = ({
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
      fragmentType="PROGRAM"
      canonicalPath={canonicalPath}
      label={label}
    >
      {(profile, loading, error) => {
        if (loading) return null;
        if (error) return null;
        return <ProgramProfileDashboard profile={profile} />;
      }}
    </FragmentProfileModal>
  );
};
