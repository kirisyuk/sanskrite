import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  getRightPanelButtonStyle,
  getRightPanelButtonHoverStyle,
} from '../lib/buttonStyles';
import { HorizonPanelType } from './HorizonDrawers';

interface HorizonPanelButtonsProps {
  panelType: HorizonPanelType;
  onPanelTypeChange: (type: HorizonPanelType) => void;
  selectedNodeId: string | null;
  selectedEdgeId: string | null;
}

export const HorizonPanelButtons: React.FC<HorizonPanelButtonsProps> = ({
  panelType,
  onPanelTypeChange,
  selectedNodeId,
  selectedEdgeId,
}) => {
  const { t } = useTranslation();

  const handleButtonClick = (type: HorizonPanelType) => {
    // Toggle drawer: if already open, close it; otherwise open it
    onPanelTypeChange(panelType === type ? null : type);
  };

  const handleMouseEnter = (
    e: React.MouseEvent<HTMLButtonElement>,
    isActive: boolean,
  ) => {
    if (!isActive) {
      e.currentTarget.style.backgroundColor = getRightPanelButtonHoverStyle(false);
    }
  };

  const handleMouseLeave = (
    e: React.MouseEvent<HTMLButtonElement>,
    isActive: boolean,
  ) => {
    if (!isActive) {
      e.currentTarget.style.backgroundColor = getRightPanelButtonStyle(false).backgroundColor;
    }
  };

  return (
    <div
      className="flex flex-col items-center gap-2"
      style={{
        width: '48px',
        minWidth: '48px',
        flexShrink: 0,
        paddingTop: '76px',
      }}
    >
      {/* Object Inspector button - "i" icon - always visible */}
      <button
        type="button"
        aria-label="Object Inspector"
        title={selectedNodeId || selectedEdgeId ? "Object Inspector" : "Select a node or edge to inspect"}
        onClick={() => handleButtonClick('object-inspector')}
        className="rounded-lg flex items-center justify-center transition-all duration-300"
        style={getRightPanelButtonStyle(panelType === 'object-inspector')}
        onMouseEnter={(e) => handleMouseEnter(e, panelType === 'object-inspector')}
        onMouseLeave={(e) => handleMouseLeave(e, panelType === 'object-inspector')}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </svg>
      </button>
    </div>
  );
};

