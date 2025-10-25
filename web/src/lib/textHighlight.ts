/**
 * Text highlighting utilities for notes
 * Supports CSS Custom Highlight API with DOM fallback
 */

interface HighlightRange {
  text: string;
  noteIndex: number;
}

/**
 * Build ranges for a search string that may span multiple text nodes.
 * This works by concatenating textContent of all text nodes and mapping back to node offsets.
 */
export function findTextRangesAcrossNodes(container: Element, searchText: string): Range[] {
  const ranges: Range[] = [];
  if (!container || !searchText) return ranges;

  // Collect text nodes and lengths
  const textNodes: Array<{ node: Text; length: number }> = [];
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let node: Node | null;
  let full = '';
  while ((node = walker.nextNode())) {
    const t = node as Text;
    const chunk = t.nodeValue || '';
    textNodes.push({ node: t, length: chunk.length });
    full += chunk;
  }
  if (!full) return ranges;

  // Find all occurrences in the concatenated string
  let idx = 0;
  while (true) {
    const found = full.indexOf(searchText, idx);
    if (found === -1) break;
    const startAbs = found;
    const endAbs = found + searchText.length;

    // Map absolute positions to node + offset
    let acc = 0;
    let startNode: Text | null = null;
    let startOffset = 0;
    let endNode: Text | null = null;
    let endOffset = 0;

    for (let i = 0; i < textNodes.length; i++) {
      const { node: n, length } = textNodes[i];
      const nextAcc = acc + length;
      if (!startNode && startAbs >= acc && startAbs <= nextAcc) {
        startNode = n;
        startOffset = startAbs - acc;
      }
      if (!endNode && endAbs >= acc && endAbs <= nextAcc) {
        endNode = n;
        endOffset = endAbs - acc;
        break;
      }
      acc = nextAcc;
    }

    if (startNode && endNode) {
      const r = document.createRange();
      try {
        r.setStart(startNode, startOffset);
        r.setEnd(endNode, endOffset);
        ranges.push(r);
      } catch {}
    }

    idx = found + Math.max(1, searchText.length); // advance to avoid infinite loop on empty
  }

  return ranges;
}

/**
 * Find the first range (cross-node aware) for a note text
 */
export function findFirstRange(container: Element, noteText: string): Range | null {
  const ranges = findTextRangesAcrossNodes(container, noteText);
  return ranges.length > 0 ? ranges[0] : null;
}

/**
 * Highlight notes in a message using CSS Custom Highlight API (modern browsers)
 */
function highlightWithCustomAPI(container: Element, notes: string[]): boolean {
  if (typeof CSS === 'undefined' || !CSS.highlights) return false;

  try {
    const allRanges: Range[] = [];
    notes.forEach((noteText) => {
      const ranges = findTextRangesAcrossNodes(container, noteText);
      allRanges.push(...ranges);
    });

    CSS.highlights.set('note-highlight', new Highlight(...allRanges));
    return true;
  } catch (e) {
    console.warn('[Highlight] Custom Highlight API failed', e);
    return false;
  }
}

/**
 * Highlight notes using DOM manipulation (fallback for older browsers)
 */
function highlightWithDOM(container: Element, notes: string[]): void {
  // Remove existing highlights
  container.querySelectorAll('.note-highlight').forEach((el) => {
    const parent = el.parentNode;
    if (parent) {
      parent.replaceChild(document.createTextNode(el.textContent || ''), el);
      parent.normalize(); // Merge adjacent text nodes
    }
  });

  notes.forEach((noteText, idx) => {
    const ranges = findTextRangesAcrossNodes(container, noteText);
    // Process ranges in reverse to maintain correct positions
    ranges.reverse().forEach((range) => {
      const mark = document.createElement('mark');
      mark.className = 'note-highlight';
      mark.setAttribute('data-note-index', String(idx));
      mark.title = `Note ${idx + 1}`;
      try {
        range.surroundContents(mark);
      } catch (e) {
        try {
          const contents = range.extractContents();
          mark.appendChild(contents);
          range.insertNode(mark);
        } catch {}
      }
    });
  });
}

/**
 * Clear all highlights from a container
 */
export function clearHighlights(container: Element): void {
  container.querySelectorAll('.note-highlight').forEach((el) => {
    const parent = el.parentNode;
    if (parent) {
      parent.replaceChild(document.createTextNode(el.textContent || ''), el);
      parent.normalize();
    }
  });
}

/**
 * Apply a single global CSS Highlight containing ranges across all containers.
 */
export function applyGlobalHighlight(pairs: Array<{ container: Element; notes: string[] }>): boolean {
  if (typeof CSS === 'undefined' || !CSS.highlights) return false;
  try {
    const all: Range[] = [];
    for (const { container, notes } of pairs) {
      const valid = (notes || []).filter((n) => n && n.trim().length > 0);
      for (const note of valid) {
        all.push(...findTextRangesAcrossNodes(container, note));
      }
    }
    CSS.highlights.set('note-highlight', new Highlight(...all));
    return true;
  } catch (e) {
    console.warn('[Highlight] applyGlobalHighlight failed', e);
    return false;
  }
}

export function clearGlobalHighlight(): void {
  if (typeof CSS !== 'undefined' && CSS.highlights) {
    try { CSS.highlights.delete('note-highlight'); } catch {}
  }
}

/**
 * Highlight notes in a message container
 */
export function highlightNotes(container: Element, notes: string[]): void {
  if (!container || !notes || notes.length === 0) {
    clearHighlights(container);
    return;
  }
  const validNotes = notes.filter((n) => n && n.trim().length > 0);
  if (validNotes.length === 0) {
    clearHighlights(container);
    return;
  }
  const customAPISuccess = highlightWithCustomAPI(container, validNotes);
  if (!customAPISuccess) {
    highlightWithDOM(container, validNotes);
  }
}

