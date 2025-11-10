import { useState, useCallback } from 'react';

export type RightPanelType = 'workspace' | 'context' | 'artifacts' | 'analysis' | 'experimental' | null;
export type LeftPanelType = 'user' | 'flow' | null;
export type AnalysisTabType = 'user' | 'flow';

export function usePanelsManagement() {
  const [rightPanelType, setRightPanelType] = useState<RightPanelType>(null);
  const [leftPanel, setLeftPanel] = useState<LeftPanelType>(null);
  const [analysisTab, setAnalysisTab] = useState<AnalysisTabType>('user');
  const [drawerWidth, setDrawerWidth] = useState(400);

  const closeAllRightPanels = useCallback(() => {
    setRightPanelType(null);
  }, []);

  const getPanelVisibilityStyles = useCallback((isVisible: boolean) => {
    return {
      opacity: isVisible ? 1 : 0,
      pointerEvents: isVisible ? ('auto' as const) : ('none' as const),
      transition: 'opacity 0.2s ease-in-out',
    };
  }, []);

  return {
    rightPanelType,
    setRightPanelType,
    leftPanel,
    setLeftPanel,
    analysisTab,
    setAnalysisTab,
    drawerWidth,
    setDrawerWidth,
    closeAllRightPanels,
    getPanelVisibilityStyles,
  };
}

