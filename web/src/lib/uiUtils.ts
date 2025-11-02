/**
 * UI utility functions for common UI operations
 */

/**
 * Get user's preferred language from localStorage
 * Returns lowercase 2-letter code (default: 'en')
 */
export function getPreferredLanguage(): string {
  if (typeof window === 'undefined') return 'en';
  const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
  return preferred && preferred.length === 2 ? preferred : 'en';
}

/**
 * Convert language code to full language name for prompts
 */
export function getLanguageName(langCode: string): string {
  const code = langCode.toLowerCase();
  switch (code) {
    case 'ru': return 'Russian';
    case 'en': return 'English';
    case 'kk':
    case 'kz': return 'Kazakh';
    default: return 'English';
  }
}

/**
 * Get anchor rectangle for modals/palettes, with fallback chain
 * Returns rect with top, left, width, height, or null if element not found
 */
export function getAnchorRect(): { top: number; left: number; width: number; height: number } | null {
  try {
    const el = (document.querySelector('#unfold-request-container') || 
                document.querySelector('#app') || 
                document.body) as HTMLElement;
    const rect = el.getBoundingClientRect();
    return { top: rect.top - 8, left: rect.left, width: rect.width, height: rect.height };
  } catch {
    return null;
  }
}

/**
 * Generate SX system instruction (short version)
 * Used for edit operations and simple interactions
 */
export function getSxSystemInstructionShort(): string {
  return `Hidden-instructions protocol (do not show to users):\n- When you need to output service instructions (entities, metrics, watchlist, relations), emit ONLY HTML comments <!-- @sx:{JSON} -->.`;
}

/**
 * Generate SX system instruction (full version)
 * Used for agent interactions with full context
 */
export function getSxSystemInstructionFull(): string {
  return (
    `Hidden-instructions protocol (do not show to users):\n` +
    `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
    `- JSON must be valid and match v: "1" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
    `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
    `- Examples: <!-- @sx:{"v":"1","type":"entity_mark","id":"ent-001","kind":"person","name":"…","description":"…"} -->`
  );
}

