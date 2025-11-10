import { useState, useEffect, useCallback } from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

/**
 * Hook для управления состоянием редактирования текста
 */
export function useTextEditing() {
  const [frozenEdits, setFrozenEdits] = useState<Record<string, boolean>>({});
  const [editErrors, setEditErrors] = useState<Record<string, string | null>>({});
  const [editLast, setEditLast] = useState<Record<string, { selected: string; request: string }>>({});
  const [editSeconds, setEditSeconds] = useState(0);
  const [editHighlight, setEditHighlight] = useState<Record<string, { start: number; end: number }>>({});

  // Timer для отслеживания времени редактирования
  useEffect(() => {
    const anyFrozen = Object.values(frozenEdits).some(Boolean);
    if (!anyFrozen) {
      setEditSeconds(0);
      return;
    }
    const id = window.setInterval(() => setEditSeconds((s) => s + 1), 1000);
    return () => {
      window.clearInterval(id);
    };
  }, [frozenEdits]);

  const handleEditStart = useCallback((interactionId: string) => {
    setFrozenEdits((s) => ({ ...s, [interactionId]: true }));
  }, []);

  const handleEditFinish = useCallback((interactionId: string) => {
    setFrozenEdits((s) => ({ ...s, [interactionId]: false }));
  }, []);

  const handleEditError = useCallback((interactionId: string, error: string) => {
    setEditErrors((s) => ({ ...s, [interactionId]: error }));
  }, []);

  const handleEditUpdate = useCallback((interactionId: string, newContent: string) => {
    useGenaiChatStore.setState(
      (s) =>
        ({
          interactions: s.interactions.map((x) =>
            x.id === interactionId
              ? { ...x, contentSanitized: newContent }
              : x,
          ),
        }) as any,
    );
    setEditErrors((s) => ({ ...s, [interactionId]: null }));
  }, []);

  const handleSaveRequest = useCallback((interactionId: string, selectedText: string, request: string) => {
    setEditLast((s) => ({
      ...s,
      [interactionId]: { selected: selectedText, request },
    }));
  }, []);

  return {
    frozenEdits,
    editErrors,
    editLast,
    editSeconds,
    editHighlight,
    setEditHighlight,
    handleEditStart,
    handleEditFinish,
    handleEditError,
    handleEditUpdate,
    handleSaveRequest,
  };
}

