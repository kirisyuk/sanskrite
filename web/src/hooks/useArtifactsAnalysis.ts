import { useCallback } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

/**
 * Hook для управления анализом артефактов (entities, facts, events, metrics, decisions)
 */
export function useArtifactsAnalysis() {
  const genaiChat = useGenaiChatStore();

  const createAnalyzeHandler = useCallback(
    (type: 'entities' | 'facts' | 'events' | 'metrics' | 'decisions') => {
      return async () => {
        try {
          const pid = await genaiChat.ensureProject();
          try {
            if ((window as any).wsSubscribeProject && pid) {
              await (window as any).wsSubscribeProject(pid, { timeoutMs: 2500 });
            }
          } catch (err) {
            console.error(`[Analyze${type}] wsSubscribeProject failed`, err);
            throw err;
          }
          
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
    [genaiChat],
  );

  return {
    handleAnalyzeEntities: createAnalyzeHandler('entities'),
    handleAnalyzeFacts: createAnalyzeHandler('facts'),
    handleAnalyzeEvents: createAnalyzeHandler('events'),
    handleAnalyzeMetrics: createAnalyzeHandler('metrics'),
    handleAnalyzeDecisions: createAnalyzeHandler('decisions'),
  };
}


