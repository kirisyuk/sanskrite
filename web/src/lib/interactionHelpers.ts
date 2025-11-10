/**
 * Утилиты для работы с bidirectional mapping между local и server IDs
 */

import { api } from './api';

/**
 * Создает reverse map для поиска local IDs по server ID
 */
export function createReverseMap(
  persistedByLocalId: Record<string, string>,
): Record<string, string[]> {
  const reverseMap: Record<string, string[]> = {};
  Object.entries(persistedByLocalId).forEach(([local, server]) => {
    const serverKey = server as string;
    if (!reverseMap[serverKey]) {
      reverseMap[serverKey] = [];
    }
    reverseMap[serverKey].push(local);
  });
  return reverseMap;
}

/**
 * Находит все local IDs для данного server ID
 */
export function getLocalIdsForServerId(
  serverId: string,
  persistedByLocalId: Record<string, string>,
): string[] {
  const reverseMap = createReverseMap(persistedByLocalId);
  return reverseMap[serverId] || [];
}

/**
 * Находит eval.bias interaction для данного message ID
 */
export function findEvalBiasInteraction(
  messageId: string,
  interactions: any[],
  persistedByLocalId: Record<string, string>,
): any | null {
  const localIds = getLocalIdsForServerId(messageId, persistedByLocalId);
  
  return (
    [...interactions]
      .reverse()
      .find(
        (n) =>
          n.subkind === 'eval.bias' &&
          (n.replyTo === messageId || localIds.includes(n.replyTo || '')),
      ) || null
  );
}

/**
 * Проверяет, закреплено ли взаимодействие
 */
export function isInteractionPinned(interaction: any): boolean {
  return !!interaction?.metadata?.pinned;
}

/**
 * Переключает статус закрепления взаимодействия
 */
export async function toggleInteractionPin(
  projectId: string,
  interactionId: string,
  currentlyPinned: boolean,
): Promise<void> {
  await api.updateProjectInteraction(projectId, interactionId, {
    metadata: {
      pinned: !currentlyPinned,
    },
  });
}

