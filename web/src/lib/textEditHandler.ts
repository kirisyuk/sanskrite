/**
 * Обработчик редактирования выделенного текста в сообщениях
 * Вынесено из DiscoveryPage для улучшения читаемости и переиспользования
 */

import React from 'react';
import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { HATL_ONE_INSTRUCTIONS } from './hatlInstructions';
import {
  getPreferredLanguage,
  getSxSystemInstructionShort,
} from './uiUtils';
import { replaceTextInMarkdown } from './markdownNormalizer';
import AgentPalette from '../components/AgentPalette';

export interface EditTextOptions {
  interactionId: string;
  selectedText: string;
  fullText: string;
  displayedText: string;
  agentName: string;
  agentProfile?: any;
  onUpdate: (interactionId: string, newContent: string) => void;
  onError: (interactionId: string, error: string) => void;
  onStart: (interactionId: string) => void;
  onFinish: (interactionId: string) => void;
  onSaveRequest?: (interactionId: string, selectedText: string, request: string) => void;
}

/**
 * Очищает ответ модели от code fences и кавычек
 */
function stripFences(s: string): string {
  let t = s.trim();
  t = t.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
  t = t
    .replace(/^```[\s\S]*?```$/m, (m) =>
      m
        .replace(/^```[a-zA-Z]*\n?/, '')
        .replace(/\n?```$/, ''),
    )
    .trim();
  if (
    (t.startsWith('"') && t.endsWith('"')) ||
    (t.startsWith("'") && t.endsWith("'"))
  ) {
    t = t.slice(1, -1);
  }
  return t.trim();
}

/**
 * Обрабатывает редактирование текста через AgentPalette
 */
export async function handleEditTextWithPalette(
  options: EditTextOptions,
): Promise<void> {
  const {
    interactionId,
    selectedText,
    fullText,
    displayedText,
    agentName,
    agentProfile,
    onUpdate,
    onError,
    onStart,
    onFinish,
    onSaveRequest,
  } = options;

  try {
    const preferredLang = getPreferredLanguage();
    const anchor = (await import('./uiUtils')).getAnchorRect();

    const ctxNode = React.createElement(
      'div',
      null,
      React.createElement(
        'div',
        null,
        React.createElement('b', null, 'Fragment:'),
        ' ',
        selectedText.slice(0, 200),
        selectedText.length > 200 ? '…' : '',
      ),
      React.createElement(
        'div',
        { style: { marginTop: 4 } },
        React.createElement('b', null, 'Answer (head):'),
        ' ',
        (fullText || '').slice(0, 160),
        fullText.length > 160 ? '…' : '',
      ),
    );

    const submit = async (request: string) => {
      if (!request.trim()) return;
      onStart(interactionId);
      
      // Save request for retry
      if (onSaveRequest) {
        onSaveRequest(interactionId, selectedText, request);
      }

      const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. We provide the full assistant answer (FULL_ANSWER) and a specific fragment (SELECTED_FRAGMENT) the user wants to modify. The user request is (USER_REQUEST).\n\nReturn ONLY the updated fragment. Preserve the fragment's existing formatting and structure where possible, and use Markdown appropriately (headings ###, lists, emphasis, blank lines) if it helps readability and does not contradict the request. Do not add any preface, labels, or explanations. Do not wrap the result in code fences. Do NOT return JSON.\n\nFULL_ANSWER:\n${fullText}\n\nSELECTED_FRAGMENT:\n${selectedText}\n\nUSER_REQUEST:\n${request}\n`;

      const settings = useGenAISettings.getState();
      const useModel = agentProfile?.model || settings.model;
      const sxSystemInstruction = getSxSystemInstructionShort();

      const genaiOptions: any = {
        profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
        modelOverride: useModel,
        groundingEnabled: settings.groundingEnabled,
        temperature: settings.temperature,
        maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
        systemInstruction:
          [
            settings.systemInstruction || '',
            HATL_ONE_INSTRUCTIONS,
            sxSystemInstruction,
          ]
            .filter(Boolean)
            .join('\n\n')
            .trim() || undefined,
      };

      try {
        const started = performance.now();
        const originalIndex = displayedText.indexOf(selectedText);
        const small = selectedText.length < 200;

        console.log('[EditSelected] sending', {
          provider: agentProfile?.provider,
          model: useModel,
          promptChars: prompt.length,
          fragmentChars: selectedText.length,
          answerChars: fullText.length,
          originalIndex,
          requestPreview: small
            ? request
            : `${request.slice(0, 120)}${request.length > 120 ? '…' : ''}`,
          fragmentPreview: small
            ? selectedText
            : `${selectedText.slice(0, 120)}…${selectedText.slice(-120)}`,
        });

        const res = await api.genaiGenerate({
          model: useModel,
          prompt,
          strictJSONEnabled: false,
          maxOutputTokens: genaiOptions.maxOutputTokens,
          webSearchEnabled: false,
          systemInstruction: genaiOptions.systemInstruction,
        });

        const rawOut = (res?.content || '').toString();
        const cleaned = stripFences(rawOut);
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

        const eq = updated === selectedText;
        const headFrom = selectedText.slice(0, 120);
        const tailFrom =
          selectedText.length > 240
            ? selectedText.slice(-120)
            : selectedText.slice(
                Math.max(0, selectedText.length - 120),
              );
        const headTo = updated.slice(0, 120);
        const tailTo =
          updated.length > 240
            ? updated.slice(-120)
            : updated.slice(Math.max(0, updated.length - 120));
        const delta = Math.abs(updated.length - selectedText.length);

        let newContentSanitized = '';
        try {
          const md = fullText;
          newContentSanitized = replaceTextInMarkdown(
            md,
            selectedText,
            updated,
          );
        } catch {
          newContentSanitized = displayedText.replace(selectedText, updated);
        }

        onUpdate(interactionId, newContentSanitized);

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
          insertedPreview:
            updated.length < 200
              ? updated
              : `${updated.slice(0, 120)}…${updated.slice(-80)}`,
        });

        if (eq) {
          onError(
            interactionId,
            'No change produced. Try clarifying the edit request.',
          );
        }
      } catch (e: any) {
        console.error('[EditSelected] error', {
          error: e?.message || String(e),
        });
        onError(interactionId, e?.message || 'Edit failed');
      } finally {
        onFinish(interactionId);
      }
    };

    // Mount AgentPalette (edit)
    const mount = document.createElement('div');
    document.body.appendChild(mount);
    const rootMod = await import('react-dom/client');
    const root = rootMod.createRoot(mount);
    const onClose = () => {
      try {
        root.unmount();
        mount.remove();
      } catch {}
    };
    root.render(
      React.createElement(AgentPalette as any, {
        mode: 'edit',
        visible: true,
        title: 'Edit selected fragment',
        subtitle: agentName,
        anchorRect: anchor,
        contextNode: ctxNode,
        submitLabel: 'Apply',
        onClose,
        onSubmitAsk: (t: string) => {
          onClose();
          submit(t);
        },
      }),
    );
  } catch (e: any) {
    console.warn('Edit selection failed', e);
    onError(interactionId, e?.message || 'Edit failed');
    onFinish(interactionId);
  }
}

/**
 * Обрабатывает редактирование текста напрямую (без палитры)
 * Используется для повторной попытки после ошибки
 */
export async function handleEditTextDirect(
  options: EditTextOptions & { request: string },
): Promise<void> {
  const {
    interactionId,
    selectedText,
    fullText,
    displayedText,
    agentName,
    agentProfile,
    request,
    onUpdate,
    onError,
    onStart,
    onFinish,
  } = options;

  onStart(interactionId);

  try {
    const preferredLang = getPreferredLanguage();
    const settings = useGenAISettings.getState();
    const useModel = agentProfile?.model || settings.model;
    const sxSystemInstruction = getSxSystemInstructionShort();

    const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. We provide the full assistant answer (FULL_ANSWER) and a specific fragment (SELECTED_FRAGMENT) the user wants to modify. The user request is (USER_REQUEST).\n\nReturn ONLY the updated fragment. Preserve the fragment's existing formatting and structure where possible, and use Markdown appropriately (headings ###, lists, emphasis, blank lines) if it helps readability and does not contradict the request. Do not add any preface, labels, or explanations. Do not wrap the result in code fences. Do NOT return JSON.\n\nFULL_ANSWER:\n${fullText}\n\nSELECTED_FRAGMENT:\n${selectedText}\n\nUSER_REQUEST:\n${request}\n`;

    const genaiOptions: any = {
      profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
      modelOverride: useModel,
      groundingEnabled: settings.groundingEnabled,
      temperature: settings.temperature,
      maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
      systemInstruction:
        [
          settings.systemInstruction || '',
          HATL_ONE_INSTRUCTIONS,
          sxSystemInstruction,
        ]
          .filter(Boolean)
          .join('\n\n')
          .trim() || undefined,
    };

    const res = await api.genaiGenerate({
      model: useModel,
      prompt,
      strictJSONEnabled: false,
      maxOutputTokens: genaiOptions.maxOutputTokens,
      webSearchEnabled: false,
      systemInstruction: genaiOptions.systemInstruction,
    });

    const rawOut = (res?.content || '').toString();
    let cleaned = stripFences(rawOut);

    try {
      const obj = JSON.parse(cleaned);
      if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
        const k = Object.keys(obj)[0];
        if (k) {
          cleaned = String((obj as any)[k] ?? '').trim();
        }
      }
    } catch {}

    const updated = stripFences(cleaned);
    if (!updated) throw new Error('Empty model response');

    let newContentSanitized = '';
    try {
      const md = fullText;
      newContentSanitized = replaceTextInMarkdown(md, selectedText, updated);
    } catch {
      newContentSanitized = displayedText.replace(selectedText, updated);
    }

    onUpdate(interactionId, newContentSanitized);
  } catch (err: any) {
    onError(interactionId, err?.message || 'Edit failed');
  } finally {
    onFinish(interactionId);
  }
}

