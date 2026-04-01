/**
 * Утилиты для обработки highlight в тексте сообщений
 */

export interface EditHighlight {
  start: number;
  end: number;
}

/**
 * Применяет highlight к тексту сообщения для отображения редактирования
 */
export function applyEditHighlight(
  content: string,
  highlight: EditHighlight | undefined,
): string {
  if (!highlight) return content;

  const { start, end } = highlight;
  if (
    typeof start !== 'number' ||
    typeof end !== 'number' ||
    start < 0 ||
    end <= start ||
    end > content.length
  ) {
    return content;
  }

  const pre = content.slice(0, start);
  const mid = content.slice(start, end);
  const post = content.slice(end);

  // Escape HTML и обернуть в span с highlight стилем
  const escapedMid = mid.replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const span = `<span class="nv-edit-hl" style="background: rgba(255, 230, 150, 0.6); padding: 0 2px; border-radius: 3px;">${escapedMid}</span>`;

  return pre + span + post;
}

