import { useCallback, useRef } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

/**
 * Hook для управления анализом артефактов (entities, facts, events, metrics, decisions)
 */
export function useArtifactsAnalysis() {
  const genaiChat = useGenaiChatStore();

  const waitForWsSubscribed = useCallback(
    async (projectId: string, timeoutMs = 1500) => {
      const start = Date.now();
      while (true) {
        try {
          const ws = (await import('../stores/wsStore')).useWsStore.getState();
          if (ws.connected && ws.subscribedProjectId === projectId) break;
        } catch {}
        if (Date.now() - start > timeoutMs) break;
        await new Promise((r) => setTimeout(r, 60));
      }
    },
    [],
  );

  const createAnalyzeHandler = useCallback(
    (type: 'entities' | 'facts' | 'events' | 'metrics' | 'decisions') => {
      return async () => {
        try {
          const pid = await genaiChat.ensureProject();
          try {
            if ((window as any).wsSubscribeProject && pid) {
              (window as any).wsSubscribeProject(pid);
              await waitForWsSubscribed(pid);
            }
          } catch {}
          
          const api = (await import('../lib/api')).api;
          let res;
          
          switch (type) {
            case 'entities':
              res = await api.analyzeEntities(pid, '');
              break;
            case 'facts':
              res = await api.analyzeFacts(pid, '');
              break;
            case 'events':
              res = await api.analyzeEvents(pid, '');
              break;
            case 'metrics':
              res = await api.analyzeMetrics(pid, '');
              break;
            case 'decisions':
              res = await api.analyzeDecisions(pid, '');
              break;
          }
          
          try {
            console.log(`[Analyze${type.charAt(0).toUpperCase() + type.slice(1)}] queued:`, res);
          } catch {}
        } catch (error) {
          console.error(`[Analyze${type.charAt(0).toUpperCase() + type.slice(1)}] failed:`, error);
        }
      };
    },
    [genaiChat, waitForWsSubscribed],
  );

  return {
    handleAnalyzeEntities: createAnalyzeHandler('entities'),
    handleAnalyzeFacts: createAnalyzeHandler('facts'),
    handleAnalyzeEvents: createAnalyzeHandler('events'),
    handleAnalyzeMetrics: createAnalyzeHandler('metrics'),
    handleAnalyzeDecisions: createAnalyzeHandler('decisions'),
  };
}

