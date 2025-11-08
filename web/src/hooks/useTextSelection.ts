import { useEffect, RefObject } from 'react';
import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { api } from '../lib/api';
import AgentPalette from '../components/AgentPalette';
import { HATL_ONE_INSTRUCTIONS } from '../lib/hatlInstructions';
import { getPreferredLanguage, getAnchorRect, getSxSystemInstructionShort } from '../lib/uiUtils';

interface TextSelectionHandlerProps {
  containerRef: RefObject<HTMLDivElement>;
  setFrozenEdits: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  setEditErrors: React.Dispatch<React.SetStateAction<Record<string, string | null>>>;
}

/**
 * Hook для обработки выделения текста в сообщениях чата
 * Создает плавающие кнопки для редактирования и добавления в заметки
 */
export function useTextSelection({
  containerRef,
  setFrozenEdits,
  setEditErrors,
}: TextSelectionHandlerProps) {
  const selectionBtnRef = React.useRef<HTMLButtonElement | null>(null);
  const isSelectingRef = React.useRef<boolean>(false);
  const selectionMenuRef = React.useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const containerEl = container as HTMLElement;

    function isNodeInside(el: Node | null, root: HTMLElement): boolean {
      if (!el) return false;
      const element = (el.nodeType === 3 ? (el.parentElement as HTMLElement | null) : (el as HTMLElement));
      return !!element && (element === root || root.contains(element));
    }

    // Ensure floating button element exists once
    if (!selectionBtnRef.current) {
      const btn = document.createElement('button');
      btn.setAttribute('type', 'button');
      btn.setAttribute('aria-label', 'Selection action');
      btn.setAttribute('title', 'Selection action');
      btn.tabIndex = -1;
      Object.assign(btn.style, {
        position: 'fixed',
        width: '28px',
        height: '28px',
        borderRadius: '9999px',
        backgroundColor: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        color: 'var(--text-heading)',
        display: 'none',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: '60',
        boxShadow: '0 2px 8px rgba(0,0,0,0.25)',
        userSelect: 'none',
      } as CSSStyleDeclaration);
      btn.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14" stroke-width="2" stroke-linecap="round"/></svg>';
      btn.addEventListener('mousedown', (e) => { e.preventDefault(); });
      btn.addEventListener('mouseenter', () => { if (selectionBtnRef.current) selectionBtnRef.current.style.backgroundColor = 'var(--white-10)'; });
      btn.addEventListener('mouseleave', () => { if (selectionBtnRef.current) selectionBtnRef.current.style.backgroundColor = 'var(--bg-elevated)'; });
      document.body.appendChild(btn);
      selectionBtnRef.current = btn as HTMLButtonElement;
    }

    // Ensure floating menu exists once
    if (!selectionMenuRef.current) {
      const menu = document.createElement('div');
      Object.assign(menu.style, {
        position: 'fixed',
        display: 'none',
        gap: '6px',
        padding: '6px',
        borderRadius: '10px',
        backgroundColor: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        boxShadow: '0 4px 14px rgba(0,0,0,0.25)',
        zIndex: '61',
        alignItems: 'center',
      } as CSSStyleDeclaration);

      const mkBtn = (label: string, icon: 'edit' | 'note') => {
        const b = document.createElement('button');
        b.setAttribute('type', 'button');
        Object.assign(b.style, {
          fontSize: '12px',
          lineHeight: '18px',
          padding: '4px 8px',
          borderRadius: '8px',
          background: 'transparent',
          color: 'var(--text-heading)',
          border: '1px solid var(--overlay-line-90)',
          display: 'inline-flex',
          alignItems: 'center',
          gap: '6px'
        } as CSSStyleDeclaration);
        // Build icon using SVG namespace (reliable rendering)
        const ns = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(ns, 'svg');
        svg.setAttribute('viewBox', '0 0 24 24');
        svg.setAttribute('width', '14');
        svg.setAttribute('height', '14');
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('stroke-width', '2');
        svg.setAttribute('stroke-linecap', 'round');
        svg.setAttribute('stroke-linejoin', 'round');
        if (icon === 'edit') {
          const p1 = document.createElementNS(ns, 'path');
          p1.setAttribute('d', 'M3 21l3-1 11-11-2-2L4 18l-1 3z');
          const p2 = document.createElementNS(ns, 'path');
          p2.setAttribute('d', 'M14 4l2 2');
          svg.appendChild(p1); svg.appendChild(p2);
        } else {
          const rect = document.createElementNS(ns, 'rect');
          rect.setAttribute('x', '4'); rect.setAttribute('y', '3');
          rect.setAttribute('width', '16'); rect.setAttribute('height', '18');
          rect.setAttribute('rx', '2');
          const p1 = document.createElementNS(ns, 'path');
          p1.setAttribute('d', 'M12 8v8');
          const p2 = document.createElementNS(ns, 'path');
          p2.setAttribute('d', 'M8 12h8');
          svg.appendChild(rect); svg.appendChild(p1); svg.appendChild(p2);
        }
        const labelSpan = document.createElement('span');
        labelSpan.textContent = label;
        b.appendChild(svg);
        b.appendChild(labelSpan);
        // Keep selection while pressing menu buttons
        b.addEventListener('mousedown', (e) => { e.preventDefault(); });
        // Handle actions
        if (icon === 'edit') {
          b.addEventListener('click', async () => {
            try {
              const sel = window.getSelection();
              if (!sel || sel.isCollapsed || sel.rangeCount === 0) return;
              const range = sel.getRangeAt(0);
              const startEl = (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null;
              if (!startEl) return;
              const markdownRoot = startEl.closest('.markdown') as HTMLElement | null;
              if (!markdownRoot) return;
              const interId = markdownRoot.getAttribute('data-interaction-id') || '';
              const selectedText = sel.toString();
              const inter = useGenaiChatStore.getState().interactions.find((it) => it.id === interId);
              const full = (inter?.contentSanitized || inter?.content || '').toString();
              const displayed = (markdownRoot.innerText || '').toString();
              const meta = useGenaiChatStore.getState().messageMeta[interId];
              const agentName = meta?.headerText || 'agent';
              const prof = meta?.agentProfile;
              const preferredLang = getPreferredLanguage();
              // Open AgentPalette in edit mode anchored to input area
              const anchor = getAnchorRect();
              const ctxNode = React.createElement('div', null,
                React.createElement('div', null, React.createElement('b', null, 'Fragment:'), ' ', selectedText.slice(0, 200), selectedText.length > 200 ? '…' : ''),
                React.createElement('div', { style: { marginTop: 4 } }, React.createElement('b', null, 'Answer (head):'), ' ', (full || '').slice(0, 160), (full || '').length > 160 ? '…' : '')
              );
              const submit = async (request: string) => {
                if (!request.trim()) return;
                setFrozenEdits((s) => ({ ...s, [interId]: true }));
                // Plain text instruction: return only updated fragment, no formatting wrappers
                const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. We provide the full assistant answer (FULL_ANSWER) and a specific fragment (SELECTED_FRAGMENT) the user wants to modify. The user request is (USER_REQUEST).\n\nReturn ONLY the updated fragment. Preserve the fragment's existing formatting and structure where possible, and use Markdown appropriately (headings ###, lists, emphasis, blank lines) if it helps readability and does not contradict the request. Do not add any preface, labels, or explanations. Do not wrap the result in code fences. Do NOT return JSON.\n\nFULL_ANSWER:\n${full}\n\nSELECTED_FRAGMENT:\n${selectedText}\n\nUSER_REQUEST:\n${request}\n`;
                const settings = useGenAISettings.getState();
                const useModel = prof?.model || settings.model;
                const sxSystemInstruction = getSxSystemInstructionShort();
                const options: any = {
                  profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                  modelOverride: useModel,
                  groundingEnabled: settings.groundingEnabled,
                  temperature: settings.temperature,
                  maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
                  systemInstruction: [settings.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined,
                };
                try {
                  const started = performance.now();
                  const originalIndex = displayed.indexOf(selectedText);
                  const small = selectedText.length < 200;
                  console.log('[EditSelected] sending', {
                    provider: prof?.provider,
                    model: useModel,
                    promptChars: prompt.length,
                    fragmentChars: selectedText.length,
                    answerChars: full.length,
                    originalIndex,
                    requestPreview: small ? request : `${request.slice(0, 120)}${request.length>120?'…':''}`,
                    fragmentPreview: small ? selectedText : `${selectedText.slice(0, 120)}…${selectedText.slice(-120)}`
                  });
                  const res = await api.genaiGenerate({ model: useModel, prompt, strictJSONEnabled: false, maxOutputTokens: options.maxOutputTokens, webSearchEnabled: false, systemInstruction: options.systemInstruction });
                  // Normalize output: strip code fences/quotes; if JSON detected, take first field value
                  const stripFences = (s: string) => {
                    let t = s.trim();
                    t = t.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
                    t = t.replace(/^```[\s\S]*?```$/m, (m) => m.replace(/^```[a-zA-Z]*\n?/,'').replace(/\n?```$/,'')).trim();
                    if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith('\'') && t.endsWith('\''))) {
                      t = t.slice(1, -1);
                    }
                    return t.trim();
                  };
                  const rawOut = (res?.content || '').toString();
                  let cleaned = stripFences(rawOut);
                  let updated = cleaned;
                  let outFormat: 'json' | 'plain' = 'plain';
                  try {
                    const obj = JSON.parse(cleaned);
                    if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
                      const k = Object.keys(obj)[0];
                      if (k) {
                        updated = String((obj as any)[k] ?? '').trim();
                        outFormat = 'json';
                      }
                    }
                  } catch {}
                  updated = stripFences(updated);
                  if (!updated) throw new Error('Empty model response');
                  // Equality / head-tail previews and char delta
                  const eq = updated === selectedText;
                  const headFrom = selectedText.slice(0, 120);
                  const tailFrom = selectedText.length > 240 ? selectedText.slice(-120) : selectedText.slice(Math.max(0, selectedText.length - 120));
                  const headTo = updated.slice(0, 120);
                  const tailTo = updated.length > 240 ? updated.slice(-120) : updated.slice(Math.max(0, updated.length - 120));
                  const delta = Math.abs(updated.length - selectedText.length);
                  let newContentSanitized = '';
                  try {
                    // Prefer replacing inside original markdown to preserve formatting
                    const md = (inter?.contentSanitized || inter?.content || '').toString();
                    // Build normalized plain string from markdown with index mapping
                    const normalized: string[] = [];
                    const mapIdx: number[] = [];
                    const len = md.length;
                    let i = 0;
                    let atLineStart = true;
                    const pushChar = (ch: string, origIdx: number) => { normalized.push(ch); mapIdx.push(origIdx); atLineStart = (ch === '\n'); };
                    while (i < len) {
                      const ch = md[i];
                      const nxt = md[i+1];
                      // Skip heading markers at line start (###, ##, #, >, - )
                      if (atLineStart) {
                        if (ch === '#') { while (i < len && md[i] === '#') i++; while (i < len && md[i] === ' ') i++; continue; }
                        if (ch === '>') { i++; if (md[i] === ' ') i++; continue; }
                        if (ch === '-' && (md[i+1] === ' ')) { i += 2; continue; }
                      }
                      // Bold/italic markers
                      if ((ch === '*' && nxt === '*') || (ch === '_' && nxt === '_')) { i += 2; continue; }
                      if (ch === '*' || ch === '_' || ch === '~') { i++; continue; }
                      // Inline code fences
                      if (ch === '`') { i++; while (i < len && md[i] !== '`') { pushChar(md[i], i); i++; } if (i < len && md[i] === '`') i++; continue; }
                      // Links [text](url)
                      if (ch === '[') {
                        // collect link text
                        i++; while (i < len && md[i] !== ']') { pushChar(md[i], i); i++; }
                        if (i < len && md[i] === ']') i++;
                        if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; }
                        continue;
                      }
                      // Images ![alt](url): skip entirely
                      if (ch === '!' && md[i+1] === '[') { i += 2; while (i < len && md[i] !== ']') i++; if (i < len && md[i] === ']') i++; if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; } continue; }
                      // Newlines pass through
                      if (ch === '\n' || ch === '\r') { pushChar('\n', i); i++; continue; }
                      // Otherwise keep char
                      pushChar(ch, i);
                      i++;
                    }
                    // Collapse whitespace in normalized for searching
                    const normStr = normalized.join('').replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ');
                    const selNorm = selectedText.replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ').trim();
                    let pos = normStr.indexOf(selNorm);
                    if (pos >= 0) {
                      const startOrig = mapIdx[pos];
                      // find endOrig by advancing selNorm length in normStr and mapping
                      const endMapIdx = mapIdx[pos + selNorm.length - 1];
                      const endOrig = (typeof endMapIdx === 'number') ? (endMapIdx + 1) : startOrig + selectedText.length;
                      const before = md.slice(0, startOrig);
                      const after = md.slice(endOrig);
                      newContentSanitized = before + updated + after;
                    } else {
                      // Fallback: replace in displayed text only (may lose formatting)
                      console.warn('[EditSelected] mapping fallback: using displayed.replace');
                      newContentSanitized = displayed.replace(selectedText, updated);
                    }
                  } catch {
                    newContentSanitized = displayed.replace(selectedText, updated);
                  }
                  useGenaiChatStore.setState((s) => ({ interactions: s.interactions.map((x) => x.id === interId ? ({ ...x, contentSanitized: newContentSanitized }) : x) } as any));
                  const finished = performance.now();
                  console.log('[EditSelected] result', {
                    ms: Math.max(1, Math.round(finished - started)),
                    updatedChars: updated.length,
                    eq,
                    delta,
                    headFrom,
                    tailFrom,
                    headTo,
                    tailTo,
                    replacedOnce: true,
                    fullIndex: originalIndex,
                    outFormat,
                    insertedPreview: updated.length < 200 ? updated : `${updated.slice(0,120)}…${updated.slice(-80)}`
                  });
                  if (eq) {
                    setEditErrors((s) => ({ ...s, [interId]: 'No change produced. Try clarifying the edit request.' }));
                  } else {
                    setEditErrors((s) => ({ ...s, [interId]: null }));
                  }
                } catch (e: any) {
                  console.error('[EditSelected] error', { error: e?.message || String(e) });
                  setEditErrors((s) => ({ ...s, [interId]: e?.message || 'Edit failed' }));
                } finally {
                  setFrozenEdits((s) => ({ ...s, [interId]: false }));
                }
              };
              // Mount AgentPalette (edit)
              const mount = document.createElement('div');
              document.body.appendChild(mount);
              const rootMod = await import('react-dom/client');
              const root = rootMod.createRoot(mount);
              const onClose = () => { try { root.unmount(); mount.remove(); } catch {} };
              root.render(
                React.createElement(AgentPalette as any, {
                  mode: 'edit', visible: true, title: 'Edit selected fragment', subtitle: agentName, anchorRect: anchor,
                  contextNode: ctxNode, submitLabel: 'Apply',
                  onClose, onSubmitAsk: (t: string) => { onClose(); submit(t); }
                })
              );
            } catch (e: any) {
              console.warn('Edit selection failed', e);
              try {
                const sel = window.getSelection();
                const range = sel?.getRangeAt(0);
                const startEl = range ? (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null : null;
                const interId = startEl?.closest('.markdown')?.getAttribute('data-interaction-id') || '';
                if (interId) setEditErrors((s) => ({ ...s, [interId]: e?.message || 'Edit failed' }));
                if (interId) setFrozenEdits((s) => ({ ...s, [interId]: false }));
              } catch {}
            }
          });
        } else {
          b.addEventListener('click', (e) => { e.preventDefault(); });
        }
        return b;
      };
      const editBtn = mkBtn('Ask to edit selected', 'edit');
      const notesBtn = mkBtn('Add to Notes', 'note');
      // Wire Add to Notes: pin if needed + append selected text to metadata.notes
      if (notesBtn) {
        notesBtn.addEventListener('click', async (e) => {
          e.preventDefault();
          try {
            const sel = window.getSelection?.()?.toString()?.trim() || '';
            if (!sel) {
              console.warn('[AddToNotes] No selection found');
              return;
            }
            const range = document.getSelection()?.getRangeAt(0);
            const startEl = range ? (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null : null;
            const interId = startEl?.closest('.markdown')?.getAttribute('data-interaction-id') || '';
            if (!interId) {
              console.warn('[AddToNotes] No interaction ID found');
              return;
            }
            const store = useGenaiChatStore.getState();
            const it = store.interactions.find((x: any) => x.id === interId);
            if (!it) {
              console.warn('[AddToNotes] Interaction not found', { interId, availableIds: store.interactions.map((x: any) => x.id).slice(0, 5) });
              return;
            }
            const projectId = store.projectId;
            if (!projectId || projectId === 'local-unfold') {
              console.warn('[AddToNotes] Invalid project ID', { projectId });
              return;
            }
            // Resolve server ID if local ID is mapped
            const map = (store as any).persistedByLocalId || {};
            const serverId = map[it.id] || it.id;
            const meta = it.metadata || {};
            const notes = Array.isArray(meta.notes) ? [...meta.notes] : [];
            notes.push(sel);
            await api.updateProjectInteraction(projectId, serverId, { metadata: { pinned: true, notes } });
            await store.hydrateInteractions();
            // Hide selection UI and clear selection after successful add
            if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
            if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
            setTimeout(() => {
              const s = window.getSelection && window.getSelection();
              if (s && s.removeAllRanges) s.removeAllRanges();
            }, 100);
          } catch (err) {
            console.warn('[AddToNotes] failed', err);
          }
        });
      }
      menu.appendChild(editBtn);
      menu.appendChild(notesBtn);
      // Prevent selection clearing when starting interaction with the menu
      menu.addEventListener('mousedown', (e) => { e.preventDefault(); });
      document.body.appendChild(menu);
      selectionMenuRef.current = menu;
    }

    // Throttle selection handling to avoid work during fast typing/caret moves
    let selectionThrottle = 0 as any;
    function handleSelectionChange() {
      // Skip if typing in inputs/textareas/contenteditable to reduce background work
      const ae = (document.activeElement as HTMLElement | null);
      if (ae && (ae.tagName === 'TEXTAREA' || ae.tagName === 'INPUT' || ae.isContentEditable)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        return;
      }
      // Basic throttle (~60fps max)
      const now = performance.now();
      if (selectionThrottle && now - selectionThrottle < 16) return;
      selectionThrottle = now;
      if (isSelectingRef.current) {
        // Do not show the button while user is dragging to select text
        return;
      }
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        return;
      }
      const range = sel.getRangeAt(0);
      if (!range) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      // Ensure selection is within assistant markdown content in this view
      const startEl = (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null;
      const endEl = (range.endContainer.nodeType === 3 ? (range.endContainer.parentElement) : (range.endContainer as Element)) as Element | null;
      if (!startEl || !endEl) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      if (!isNodeInside(startEl, containerEl) || !isNodeInside(endEl, containerEl)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      // Require that at least one side sits inside a .markdown (assistant bubble)
      const inMarkdown = !!(startEl.closest('.markdown') || endEl.closest('.markdown'));
      if (!inMarkdown) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }

      const rects = range.getClientRects();
      const rect = rects.length > 0 ? rects : [range.getBoundingClientRect()];
      if (!rect || rect.length === 0) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      let minTop = Number.POSITIVE_INFINITY;
      let maxBottom = Number.NEGATIVE_INFINITY;
      for (let i = 0; i < rect.length; i++) {
        const r = rect[i];
        if (!r) continue;
        minTop = Math.min(minTop, r.top);
        maxBottom = Math.max(maxBottom, r.bottom);
      }
      if (!isFinite(minTop) || !isFinite(maxBottom)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      const containerRect = containerEl.getBoundingClientRect();
      const buttonSize = 28;
      const gap = 8;
      const top = Math.min(Math.max((minTop + maxBottom) / 2 - buttonSize / 2, 4), window.innerHeight - buttonSize - 4);
      const leftCandidate = (containerRect?.left || 0) - gap - buttonSize;
      const left = Math.max(4, leftCandidate);
      if (selectionBtnRef.current) {
        selectionBtnRef.current.style.top = `${top}px`;
        selectionBtnRef.current.style.left = `${left}px`;
        selectionBtnRef.current.style.display = 'flex';
        selectionBtnRef.current.style.pointerEvents = 'none';
        window.setTimeout(() => {
          if (selectionBtnRef.current) selectionBtnRef.current.style.pointerEvents = 'auto';
        }, 180);
      }
      // Position menu to the right of the main button
      if (selectionMenuRef.current) {
        selectionMenuRef.current.style.top = `${Math.round(top - 2)}px`;
        selectionMenuRef.current.style.left = `${Math.round(left + buttonSize + 8)}px`;
      }
    }

    const onScrollOrResize = () => {
      // Recompute while selection exists; otherwise hide
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
      } else {
        handleSelectionChange();
      }
    };

    const onPointerDown = (e: MouseEvent) => {
      const btn = selectionBtnRef.current;
      const menu = selectionMenuRef.current;
      if ((btn && (e.target === btn || (btn.contains(e.target as Node))))) {
        // Keep selection while interacting with the button
        e.preventDefault();
        // Toggle menu visibility
        if (menu) {
          const visible = menu.style.display !== 'none';
          if (!visible) {
            // Ensure menu aligns to current button position
            const rect = btn.getBoundingClientRect();
            const top = rect.top;
            const left = rect.right + 8;
            menu.style.top = `${Math.round(top - 2)}px`;
            menu.style.left = `${Math.round(left)}px`;
            menu.style.display = 'flex';
          } else {
            menu.style.display = 'none';
          }
        }
        return;
      }
      if (menu && (e.target === menu || (menu.contains(e.target as Node)))) {
        // Interactions inside the menu should not close it or clear selection
        e.preventDefault();
        return;
      }
      // Let selection clear naturally; we'll hide on selectionchange
      // Also close menu on any outside click
      if (menu) menu.style.display = 'none';
      setTimeout(() => handleSelectionChange(), 0);
    };

    const onContainerPointerDown = () => {
      isSelectingRef.current = true;
      if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
      if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
    };
    const onDocumentPointerUp = () => {
      if (isSelectingRef.current) {
        isSelectingRef.current = false;
        // Show the button once selection is finalized
        setTimeout(() => handleSelectionChange(), 0);
      }
    };

    document.addEventListener('selectionchange', handleSelectionChange);
    window.addEventListener('scroll', onScrollOrResize, true);
    window.addEventListener('resize', onScrollOrResize);
    document.addEventListener('pointerdown', onPointerDown, true);
    containerEl.addEventListener('pointerdown', onContainerPointerDown, true);
    document.addEventListener('pointerup', onDocumentPointerUp, true);
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        const sel = document.getSelection();
        if (sel) sel.removeAllRanges();
      }
    };
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('selectionchange', handleSelectionChange);
      window.removeEventListener('scroll', onScrollOrResize, true);
      window.removeEventListener('resize', onScrollOrResize);
      document.removeEventListener('pointerdown', onPointerDown, true);
      containerEl.removeEventListener('pointerdown', onContainerPointerDown, true);
      document.removeEventListener('pointerup', onDocumentPointerUp, true);
      document.removeEventListener('keydown', onKeyDown, true);
      // Cleanup button element
      if (selectionBtnRef.current && selectionBtnRef.current.parentElement) {
        try { selectionBtnRef.current.parentElement.removeChild(selectionBtnRef.current); } catch {}
        selectionBtnRef.current = null;
      }
      if (selectionMenuRef.current && selectionMenuRef.current.parentElement) {
        try { selectionMenuRef.current.parentElement.removeChild(selectionMenuRef.current); } catch {}
        selectionMenuRef.current = null;
      }
    };
  }, [containerRef, setFrozenEdits, setEditErrors]);
}

