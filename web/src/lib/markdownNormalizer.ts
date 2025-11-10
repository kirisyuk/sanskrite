/**
 * Утилиты для нормализации markdown текста
 * Используется для поиска и замены текста в markdown с сохранением форматирования
 */

export interface NormalizedMarkdown {
  normalized: string;
  mapIdx: number[];
}

/**
 * Нормализует markdown текст, удаляя форматирование и создавая маппинг индексов
 */
export function normalizeMarkdown(markdown: string): NormalizedMarkdown {
  const normalized: string[] = [];
  const mapIdx: number[] = [];
  const len = markdown.length;
  let i = 0;
  let atLineStart = true;

  const pushChar = (ch: string, origIdx: number) => {
    normalized.push(ch);
    mapIdx.push(origIdx);
    atLineStart = ch === '\n';
  };

  while (i < len) {
    const ch = markdown[i];
    const nxt = markdown[i + 1];

    if (atLineStart) {
      // Пропускаем заголовки (#)
      if (ch === '#') {
        while (i < len && markdown[i] === '#') i++;
        while (i < len && markdown[i] === ' ') i++;
        continue;
      }
      // Пропускаем blockquotes (>)
      if (ch === '>') {
        i++;
        if (markdown[i] === ' ') i++;
        continue;
      }
      // Пропускаем списки (-)
      if (ch === '-' && markdown[i + 1] === ' ') {
        i += 2;
        continue;
      }
    }

    // Пропускаем жирный/курсив (** или __)
    if ((ch === '*' && nxt === '*') || (ch === '_' && nxt === '_')) {
      i += 2;
      continue;
    }

    // Пропускаем одинарные markdown символы (*, _, ~)
    if (ch === '*' || ch === '_' || ch === '~') {
      i++;
      continue;
    }

    // Обрабатываем inline code (`)
    if (ch === '`') {
      i++;
      while (i < len && markdown[i] !== '`') {
        pushChar(markdown[i], i);
        i++;
      }
      if (i < len && markdown[i] === '`') i++;
      continue;
    }

    // Пропускаем ссылки [text](url)
    if (ch === '[') {
      i++;
      while (i < len && markdown[i] !== ']') {
        pushChar(markdown[i], i);
        i++;
      }
      if (i < len && markdown[i] === ']') i++;
      if (i < len && markdown[i] === '(') {
        while (i < len && markdown[i] !== ')') i++;
        if (i < len && markdown[i] === ')') i++;
      }
      continue;
    }

    // Пропускаем изображения ![alt](url)
    if (ch === '!' && markdown[i + 1] === '[') {
      i += 2;
      while (i < len && markdown[i] !== ']') i++;
      if (i < len && markdown[i] === ']') i++;
      if (i < len && markdown[i] === '(') {
        while (i < len && markdown[i] !== ')') i++;
        if (i < len && markdown[i] === ')') i++;
      }
      continue;
    }

    // Обрабатываем переносы строк
    if (ch === '\n' || ch === '\r') {
      pushChar('\n', i);
      i++;
      continue;
    }

    pushChar(ch, i);
    i++;
  }

  const normalizedStr = normalized
    .join('')
    .replace(/[\u00A0\u202F]/g, ' ')
    .replace(/\r/g, '')
    .replace(/\s+/g, ' ');

  return {
    normalized: normalizedStr,
    mapIdx,
  };
}

/**
 * Находит позицию текста в нормализованном markdown и возвращает оригинальные индексы
 */
export function findTextInNormalizedMarkdown(
  markdown: string,
  searchText: string,
): { start: number; end: number } | null {
  const { normalized, mapIdx } = normalizeMarkdown(markdown);
  const normalizedSearch = searchText
    .replace(/[\u00A0\u202F]/g, ' ')
    .replace(/\r/g, '')
    .replace(/\s+/g, ' ')
    .trim();

  const pos = normalized.indexOf(normalizedSearch);
  if (pos < 0) return null;

  const startOrig = mapIdx[pos];
  const endMapIdx = mapIdx[pos + normalizedSearch.length - 1];
  const endOrig =
    typeof endMapIdx === 'number' ? endMapIdx + 1 : startOrig + searchText.length;

  return { start: startOrig, end: endOrig };
}

/**
 * Заменяет текст в markdown с сохранением форматирования
 */
export function replaceTextInMarkdown(
  markdown: string,
  searchText: string,
  replacement: string,
): string {
  const pos = findTextInNormalizedMarkdown(markdown, searchText);
  if (pos) {
    return (
      markdown.slice(0, pos.start) + replacement + markdown.slice(pos.end)
    );
  }
  // Fallback: простая замена
  return markdown.replace(searchText, replacement);
}

