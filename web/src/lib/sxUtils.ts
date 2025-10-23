import type { SxInstruction } from '../types/sx';

const ALLOWED_TYPES = new Set(['entity_mark', 'monitor_metric', 'watchlist_add', 'link_reference']);

export function sanitizeSx(text: string): string {
  try {
    return (text || '').replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim();
  } catch {
    return text;
  }
}

export function parseSxInstructionsFromContent(text: string): { sanitized: string; instructions: SxInstruction[] } {
  const instructions: SxInstruction[] = [] as any;
  if (!text || typeof text !== 'string') return { sanitized: text, instructions };
  try {
    const regex = /<!--\s*@sx:([\s\S]*?)-->/g;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      const raw = (match[1] || '').trim();
      try {
        const obj = JSON.parse(raw);
        if (!obj || obj.v !== '1' || !ALLOWED_TYPES.has(obj.type)) {
          console.warn('[sx] Dropped invalid object', obj);
          continue;
        }
        // minimal per-type checks
        if (obj.type === 'entity_mark') {
          if (typeof obj.id !== 'string' || typeof obj.name !== 'string') { console.warn('[sx] entity_mark missing id/name'); continue; }
        } else if (obj.type === 'monitor_metric') {
          if (typeof obj.id !== 'string' && typeof obj.name !== 'string') { console.warn('[sx] monitor_metric missing id/name'); continue; }
        } else if (obj.type === 'watchlist_add') {
          if (typeof obj.id !== 'string') { console.warn('[sx] watchlist_add missing id'); continue; }
          if (obj.items && !Array.isArray(obj.items)) { console.warn('[sx] watchlist_add items not array'); continue; }
        } else if (obj.type === 'link_reference') {
          if (typeof obj.relation !== 'string' || typeof obj.fromId !== 'string' || typeof obj.toId !== 'string') { console.warn('[sx] link_reference missing fields'); continue; }
        }
        instructions.push(obj as SxInstruction);
      } catch (e) {
        console.warn('[sx] JSON parse error', e);
      }
    }
  } catch {}
  const sanitized = sanitizeSx(text);
  return { sanitized, instructions };
}


