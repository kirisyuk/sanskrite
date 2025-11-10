import React from 'react';
import { ExperimentalResponsePanel } from './ExperimentalResponsePanel';
import { ExperimentalResponse } from '../lib/experimentalResponse';

interface ExperimentalPanelProps {
  response: ExperimentalResponse | null;
  loading: boolean;
}

export const ExperimentalPanel: React.FC<ExperimentalPanelProps> = ({
  response,
  loading,
}) => {
  return (
    <ExperimentalResponsePanel response={response} loading={loading} />
  );
};

