/**
 * Component for displaying Scenarios content - subscribes to store changes
 */

import React from 'react';
import { useScenarioDataStore } from '../stores/scenarioDataStore';
import { ScenarioDataDisplay } from './ScenarioDataDisplay';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface ScenariosContentProps {
  scenarioId?: string;
}

export const ScenariosContent: React.FC<ScenariosContentProps> = ({ 
  scenarioId = 'scenarios-main' 
}) => {
  const projectId = useGenaiChatStore((state) => state.projectId || '');
  
  // Use useMemo to memoize the selector result and prevent infinite loops
  // Subscribe to the data Map directly, then extract the array in useMemo
  const dataMap = useScenarioDataStore((state) => state.data);
  
  const scenarioDataList = React.useMemo(() => {
    const projectMap = dataMap.get(projectId);
    if (!projectMap) return [];
    return projectMap.get(scenarioId) || [];
  }, [dataMap, projectId, scenarioId]);
  
  // Show the latest scenario data even if it's still in progress
  // This allows showing progress stages and schema in real-time
  if (scenarioDataList.length === 0) {
    return null;
  }
  
  return (
    <div className="space-y-4 py-4" style={{ maxWidth: '100%', overflow: 'hidden' }}>
      {scenarioDataList.map((scenarioData) => (
        <ScenarioDataDisplay key={scenarioData.id} scenarioData={scenarioData} />
      ))}
    </div>
  );
};

