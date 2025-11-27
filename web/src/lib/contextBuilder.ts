/**
 * Unified context builder for LLM requests
 * Collects context from interactions and messages, limiting to 70% of model's context window
 */

import { useGenaiChatStore } from '../stores/genaiChatStore';
import { getModelContextWindow, mapProviderStrengthToModel, type GenAIModel } from '../stores/genaiSettingsStore';

export interface ContextMessage {
  role: 'user' | 'assistant';
  content: string;
}

export interface BuildContextOptions {
  /**
   * Model alias to determine context window (e.g., 'gemini_fast', 'gpt_quality')
   * If not provided, uses current settings
   */
  modelAlias?: string;
  
  /**
   * Percentage of context window to use (default: 0.7 = 70%)
   */
  contextPercentage?: number;
  
  /**
   * Whether to exclude certain interaction kinds (e.g., 'note' with specific subkinds)
   */
  excludeKinds?: Array<{ kind: string; subkindPattern?: string }>;
  
  /**
   * Whether to include agent messages (messages with agentProfile)
   */
  includeAgents?: boolean;
}

/**
 * Builds context transcript from interactions and messages, limiting to specified percentage of context window
 * 
 * @param options - Configuration options
 * @returns Object with transcript string and metadata
 */
export function buildContext(options: BuildContextOptions = {}): {
  transcript: string;
  messages: ContextMessage[];
  tokenCount: number;
  charCount: number;
  maxTokens: number;
} {
  const {
    modelAlias,
    contextPercentage = 0.7,
    excludeKinds = [],
    includeAgents = true,
  } = options;

  // Get model and context window
  let contextWindow: number;
  if (modelAlias) {
    contextWindow = getModelContextWindow(modelAlias as GenAIModel);
  } else {
    const { useGenAISettings } = require('../stores/genaiSettingsStore');
    const settings = useGenAISettings.getState();
    const alias = mapProviderStrengthToModel(settings.provider, settings.strength);
    contextWindow = getModelContextWindow(alias);
  }
  
  const maxContextTokens = contextWindow ? Math.floor(contextWindow * contextPercentage) : Infinity;
  const maxChars = Math.floor(maxContextTokens * 4); // Rough estimate: ~4 chars per token

  // Get interactions and messages
  const genaiChat = useGenaiChatStore.getState();
  const interactions = genaiChat.interactions || [];
  const messages = genaiChat.messages || [];

  // Collect messages from interactions first (they are persisted)
  const contextMessages: ContextMessage[] = [];
  
  // Process interactions
  interactions.forEach((it: any) => {
    if (!it) return;
    
    // Exclude ontology interactions (structured data, not chat content)
    if (it.kind === 'ontology') {
      return;
    }
    // Exclude layout interactions (UI state, not chat content)
    if (it.kind === 'ontology' && it.subkind === 'layout') {
      return;
    }
    
    // Exclude queued tasks interactions (they are metadata, not chat content)
    if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
      return;
    }
    
    // Check exclusions
    const shouldExclude = excludeKinds.some(excl => {
      if (it.kind !== excl.kind) return false;
      if (excl.subkindPattern && typeof it.subkind === 'string') {
        return it.subkind.startsWith(excl.subkindPattern);
      }
      return true;
    });
    if (shouldExclude) return;
    
    // Include user and assistant interactions
    if (it.kind === 'user' || it.kind === 'assistant') {
      const isUser = it.kind === 'user';
      const text = String(it.content || '').trim();
      if (text) {
        contextMessages.push({
          role: isUser ? 'user' : 'assistant',
          content: text,
        });
      }
    }
  });

  // Fallback to messages if we don't have enough interactions
  if (contextMessages.length < 3 && messages.length > 0) {
    messages.forEach((m: any) => {
      const text = String(m?.content || '').trim();
      if (!text) return;
      
      const role = String(m?.role || '').toLowerCase();
      const isUser = role === 'user' || role === 'discovery.query';
      const isAssistant = role === 'assistant' || role === 'discovery.answer' || role === 'note';
      
      // Include agent messages if requested
      const hasAgentProfile = includeAgents && (m as any).agentProfile;
      
      if ((isUser || isAssistant || hasAgentProfile) && text) {
        // Avoid duplicates - check if we already have this content
        const isDuplicate = contextMessages.some(
          cm => cm.content === text && cm.role === (isUser ? 'user' : 'assistant')
        );
        if (!isDuplicate) {
          contextMessages.push({
            role: isUser ? 'user' : 'assistant',
            content: text,
          });
        }
      }
    });
  }

  // Filter to fit within context limit (take from the end)
  let totalChars = 0;
  const selectedMessages: ContextMessage[] = [];
  
  // Go backwards from the end until we hit the limit
  for (let i = contextMessages.length - 1; i >= 0; i--) {
    const msg = contextMessages[i];
    const msgChars = msg.content.length;
    
    // Stop if adding this message would exceed limit (but allow at least one message)
    if (totalChars + msgChars > maxChars && selectedMessages.length > 0) {
      break;
    }
    
    selectedMessages.unshift(msg);
    totalChars += msgChars;
  }

  // Build transcript
  const transcript = selectedMessages.map(m => `${m.role}: ${m.content}`).join('\n\n');
  const tokenCount = Math.ceil(totalChars / 4);

  return {
    transcript,
    messages: selectedMessages,
    tokenCount,
    charCount: totalChars,
    maxTokens: maxContextTokens,
  };
}


