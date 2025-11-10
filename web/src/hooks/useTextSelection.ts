/**
 * Хук для управления выделением текста и плавающей кнопкой редактирования
 * Вынесено из DiscoveryPage для улучшения читаемости и переиспользования
 */

import { useEffect, useRef, RefObject } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { api } from '../lib/api';
import { handleEditTextWithPalette, EditTextOptions } from '../lib/textEditHandler';

export interface UseTextSelectionOptions {
  containerRef: RefObject<HTMLElement | null>;
  onEditStart?: (interactionId: string) => void;
  onEditFinish?: (interactionId: string) => void;
  onEditError?: (interactionId: string, error: string) => void;
  onEditUpdate?: (interactionId: string, newContent: string) => void;
  onSaveRequest?: (interactionId: string, selectedText: string, request: string) => void;
}

export function useTextSelection(options: UseTextSelectionOptions) {
  const {
    containerRef,
    onEditStart,
    onEditFinish,
    onEditError,
    onEditUpdate,
  } = options;

  const selectionBtnRef = useRef<HTMLButtonElement | null>(null);
  const selectionMenuRef = useRef<HTMLDivElement | null>(null);
  const isSelectingRef = useRef<boolean>(false);

  function isNodeInside(el: Node | null, root: HTMLElement): boolean {
    if (!el) return false;
    const element =
      el.nodeType === 3
        ? (el.parentElement as HTMLElement | null)
        : (el as HTMLElement);
    return !!element && (element === root || root.contains(element));
  }

  // Создание плавающей кнопки
  useEffect(() => {
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
      btn.innerHTML =
        '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14" stroke-width="2" stroke-linecap="round"/></svg>';
      btn.addEventListener('mousedown', (e) => {
        e.preventDefault();
      });
      btn.addEventListener('mouseenter', () => {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.backgroundColor = 'var(--white-10)';
      });
      btn.addEventListener('mouseleave', () => {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.backgroundColor = 'var(--bg-elevated)';
      });
      document.body.appendChild(btn);
      selectionBtnRef.current = btn as HTMLButtonElement;
    }

    return () => {
      if (selectionBtnRef.current?.parentElement) {
        selectionBtnRef.current.parentElement.removeChild(
          selectionBtnRef.current,
        );
        selectionBtnRef.current = null;
      }
    };
  }, []);

  // Создание плавающего меню
  useEffect(() => {
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
          gap: '6px',
        } as CSSStyleDeclaration);

        // Build icon using SVG namespace
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
          svg.appendChild(p1);
          svg.appendChild(p2);
        } else {
          const rect = document.createElementNS(ns, 'rect');
          rect.setAttribute('x', '4');
          rect.setAttribute('y', '3');
          rect.setAttribute('width', '16');
          rect.setAttribute('height', '18');
          rect.setAttribute('rx', '2');
          const p1 = document.createElementNS(ns, 'path');
          p1.setAttribute('d', 'M12 8v8');
          const p2 = document.createElementNS(ns, 'path');
          p2.setAttribute('d', 'M8 12h8');
          svg.appendChild(rect);
          svg.appendChild(p1);
          svg.appendChild(p2);
        }

        const labelSpan = document.createElement('span');
        labelSpan.textContent = label;
        b.appendChild(svg);
        b.appendChild(labelSpan);
        b.addEventListener('mousedown', (e) => {
          e.preventDefault();
        });

        if (icon === 'edit') {
          b.addEventListener('click', async () => {
            try {
              const sel = window.getSelection();
              if (!sel || sel.isCollapsed || sel.rangeCount === 0) return;
              const range = sel.getRangeAt(0);
              const startEl = (
                range.startContainer.nodeType === 3
                  ? range.startContainer.parentElement
                  : (range.startContainer as Element)
              ) as Element | null;
              if (!startEl) return;
              const markdownRoot = startEl.closest(
                '.markdown',
              ) as HTMLElement | null;
              if (!markdownRoot) return;
              const interId =
                markdownRoot.getAttribute('data-interaction-id') || '';
              const selectedText = sel.toString();
              const store = useGenaiChatStore.getState();
              const inter = store.interactions.find((it) => it.id === interId);
              if (!inter) return;

              const full = (
                inter?.contentSanitized ||
                inter?.content ||
                ''
              ).toString();
              const displayed = (markdownRoot.innerText || '').toString();
              const meta = store.messageMeta[interId];
              const agentName = meta?.headerText || 'agent';
              const prof = meta?.agentProfile;

              const editOptions: EditTextOptions = {
                interactionId: interId,
                selectedText,
                fullText: full,
                displayedText: displayed,
                agentName,
                agentProfile: prof,
                onUpdate: (id, newContent) => {
                  if (onEditUpdate) {
                    onEditUpdate(id, newContent);
                  } else {
                    useGenaiChatStore.setState(
                      (s) =>
                        ({
                          interactions: s.interactions.map((x) =>
                            x.id === id
                              ? { ...x, contentSanitized: newContent }
                              : x,
                          ),
                        }) as any,
                    );
                  }
                },
                onError: (id, error) => {
                  if (onEditError) {
                    onEditError(id, error);
                  }
                },
                onStart: (id) => {
                  if (onEditStart) {
                    onEditStart(id);
                  }
                },
                onFinish: (id) => {
                  if (onEditFinish) {
                    onEditFinish(id);
                  }
                },
                onSaveRequest: options.onSaveRequest,
              };

              await handleEditTextWithPalette(editOptions);
            } catch (e: any) {
              console.warn('Edit selection failed', e);
            }
          });
        }

        return b;
      };

      const editBtn = mkBtn('Ask to edit selected', 'edit');
      const notesBtn = mkBtn('Add to Notes', 'note');

      // Wire Add to Notes
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
            const startEl = range
              ? ((range.startContainer.nodeType === 3
                  ? range.startContainer.parentElement
                  : (range.startContainer as Element)) as Element | null)
              : null;
            const interId =
              startEl
                ?.closest('.markdown')
                ?.getAttribute('data-interaction-id') || '';
            if (!interId) {
              console.warn('[AddToNotes] No interaction ID found');
              return;
            }
            const store = useGenaiChatStore.getState();
            const it = store.interactions.find((x: any) => x.id === interId);
            if (!it) {
              console.warn('[AddToNotes] Interaction not found', {
                interId,
                availableIds: store.interactions
                  .map((x: any) => x.id)
                  .slice(0, 5),
              });
              return;
            }
            const projectId = store.projectId;
            if (!projectId || projectId === 'local-unfold') {
              console.warn('[AddToNotes] Invalid project ID', { projectId });
              return;
            }
            const map = (store as any).persistedByLocalId || {};
            const serverId = map[it.id] || it.id;
            const meta = it.metadata || {};
            const notes = Array.isArray(meta.notes) ? [...meta.notes] : [];
            notes.push(sel);
            await api.updateProjectInteraction(projectId, serverId, {
              metadata: { pinned: true, notes },
            });
            await store.hydrateInteractions();
            // Hide selection UI and clear selection after successful add
            if (selectionMenuRef.current)
              selectionMenuRef.current.style.display = 'none';
            if (selectionBtnRef.current)
              selectionBtnRef.current.style.display = 'none';
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
      menu.addEventListener('mousedown', (e) => {
        e.preventDefault();
      });
      document.body.appendChild(menu);
      selectionMenuRef.current = menu;
    }

    return () => {
      if (selectionMenuRef.current?.parentElement) {
        selectionMenuRef.current.parentElement.removeChild(
          selectionMenuRef.current,
        );
        selectionMenuRef.current = null;
      }
    };
  }, [onEditStart, onEditFinish, onEditError, onEditUpdate]);

  // Обработка изменений выделения
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const containerEl = container as HTMLElement;

    let selectionThrottle = 0 as any;

    function handleSelectionChange() {
      const ae = document.activeElement as HTMLElement | null;
      if (
        ae &&
        (ae.tagName === 'TEXTAREA' ||
          ae.tagName === 'INPUT' ||
          ae.isContentEditable)
      ) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current)
          selectionMenuRef.current.style.display = 'none';
        return;
      }

      const now = performance.now();
      if (selectionThrottle && now - selectionThrottle < 16) return;
      selectionThrottle = now;

      if (isSelectingRef.current) {
        return;
      }

      const sel = document.getSelection();
      if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current)
          selectionMenuRef.current.style.display = 'none';
        return;
      }

      const range = sel.getRangeAt(0);
      if (!range) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        return;
      }

      const startEl = (
        range.startContainer.nodeType === 3
          ? range.startContainer.parentElement
          : (range.startContainer as Element)
      ) as Element | null;
      const endEl = (
        range.endContainer.nodeType === 3
          ? range.endContainer.parentElement
          : (range.endContainer as Element)
      ) as Element | null;

      if (!startEl || !endEl) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        return;
      }

      if (
        !isNodeInside(startEl, containerEl) ||
        !isNodeInside(endEl, containerEl)
      ) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        return;
      }

      const inMarkdown = !!(
        startEl.closest('.markdown') || endEl.closest('.markdown')
      );
      if (!inMarkdown) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        return;
      }

      const rects = range.getClientRects();
      const rect = rects.length > 0 ? rects : [range.getBoundingClientRect()];
      if (!rect || rect.length === 0) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
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
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
        return;
      }

      const containerRect = containerEl.getBoundingClientRect();
      const buttonSize = 28;
      const gap = 8;
      const top = Math.min(
        Math.max((minTop + maxBottom) / 2 - buttonSize / 2, 4),
        window.innerHeight - buttonSize - 4,
      );
      const leftCandidate = (containerRect?.left || 0) - gap - buttonSize;
      const left = Math.max(4, leftCandidate);

      if (selectionBtnRef.current) {
        selectionBtnRef.current.style.top = `${top}px`;
        selectionBtnRef.current.style.left = `${left}px`;
        selectionBtnRef.current.style.display = 'flex';
        selectionBtnRef.current.style.pointerEvents = 'none';
        window.setTimeout(() => {
          if (selectionBtnRef.current)
            selectionBtnRef.current.style.pointerEvents = 'auto';
        }, 180);
      }

      if (selectionMenuRef.current) {
        selectionMenuRef.current.style.top = `${Math.round(top - 2)}px`;
        selectionMenuRef.current.style.left = `${Math.round(left + buttonSize + 8)}px`;
      }
    }

    const onScrollOrResize = () => {
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed) {
        if (selectionBtnRef.current)
          selectionBtnRef.current.style.display = 'none';
      } else {
        handleSelectionChange();
      }
    };

    const onPointerDown = (e: MouseEvent) => {
      const btn = selectionBtnRef.current;
      const menu = selectionMenuRef.current;
      if (btn && (e.target === btn || btn.contains(e.target as Node))) {
        e.preventDefault();
        if (menu) {
          const visible = menu.style.display !== 'none';
          if (!visible) {
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
      if (menu && (e.target === menu || menu.contains(e.target as Node))) {
        e.preventDefault();
        return;
      }
      if (menu) menu.style.display = 'none';
      setTimeout(() => handleSelectionChange(), 0);
    };

    const onContainerPointerDown = () => {
      isSelectingRef.current = true;
      if (selectionBtnRef.current)
        selectionBtnRef.current.style.display = 'none';
      if (selectionMenuRef.current)
        selectionMenuRef.current.style.display = 'none';
    };

    const onContainerPointerUp = () => {
      isSelectingRef.current = false;
      setTimeout(() => handleSelectionChange(), 50);
    };

    document.addEventListener('selectionchange', handleSelectionChange);
    window.addEventListener('scroll', onScrollOrResize, { passive: true } as any);
    window.addEventListener('resize', onScrollOrResize);
    document.addEventListener('pointerdown', onPointerDown);
    container.addEventListener('pointerdown', onContainerPointerDown);
    container.addEventListener('pointerup', onContainerPointerUp);

    return () => {
      document.removeEventListener('selectionchange', handleSelectionChange);
      window.removeEventListener('scroll', onScrollOrResize as any);
      window.removeEventListener('resize', onScrollOrResize);
      document.removeEventListener('pointerdown', onPointerDown);
      container.removeEventListener('pointerdown', onContainerPointerDown);
      container.removeEventListener('pointerup', onContainerPointerUp);
    };
  }, [containerRef]);

  return {
    selectionBtnRef,
    selectionMenuRef,
  };
}
