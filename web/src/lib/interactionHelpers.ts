import { api } from './api';

/**
 * Toggle the pinned status of an interaction
 * @param projectId - Project ID
 * @param interactionId - Interaction ID
 * @param currentlyPinned - Current pinned status
 * @returns Promise with success status
 */
export async function toggleInteractionPin(
  projectId: string,
  interactionId: string,
  currentlyPinned: boolean = false
): Promise<{ success: boolean }> {
  if (currentlyPinned) {
    // Unpin: also clear notes
    return api.updateProjectInteraction(projectId, interactionId, {
      metadata: { pinned: false, notes: [] }
    });
  } else {
    // Pin
    return api.updateProjectInteraction(projectId, interactionId, {
      metadata: { pinned: true }
    });
  }
}

/**
 * Set the pinned status of an interaction
 * @param projectId - Project ID
 * @param interactionId - Interaction ID
 * @param pinned - New pinned status
 * @returns Promise with success status
 */
export async function setInteractionPin(
  projectId: string,
  interactionId: string,
  pinned: boolean
): Promise<{ success: boolean }> {
  return api.updateProjectInteraction(projectId, interactionId, {
    metadata: { pinned, notes: pinned ? undefined as any : [] }
  });
}

/**
 * Check if interaction is pinned
 * @param interaction - Interaction object
 * @returns True if pinned
 */
export function isInteractionPinned(interaction: { metadata?: { pinned?: boolean } }): boolean {
  return interaction.metadata?.pinned === true;
}

