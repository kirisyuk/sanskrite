/**
 * Maps model alias to actual model name used in API
 * Provider names must match backend/internal/http/genai.go determineProvider() output (lowercase)
 * Model names must match backend/internal/ai/model_manager.go model definitions
 */
export function mapAliasToModel(alias: string): { provider: string; model: string } | null {
  const mapping: Record<string, { provider: string; model: string }> = {
    // Gemini models
    'gemini_fast': { provider: 'gemini', model: 'gemini-2.5-flash-lite' },
    'gemini_balanced': { provider: 'gemini', model: 'gemini-2.5-flash' },
    'gemini_quality': { provider: 'gemini', model: 'gemini-2.5-pro' },
    
    // OpenAI models
    'gpt_fast': { provider: 'openai', model: 'gpt-5-nano' },
    'gpt_balanced': { provider: 'openai', model: 'gpt-5-mini' },
    'gpt_quality': { provider: 'openai', model: 'gpt-5.1' },
    
    // Grok models
    'grok_fast': { provider: 'grok', model: 'grok-4-fast-non-reasoning' },
    'grok_balanced': { provider: 'grok', model: 'grok-4-fast-reasoning' },
    'grok_quality': { provider: 'grok', model: 'grok-4-0709' },
    
    // Claude models (provider is 'anthropic' in usage metrics)
    'claude_fast': { provider: 'anthropic', model: 'claude-haiku-4-5-20251001' },
    'claude_balanced': { provider: 'anthropic', model: 'claude-sonnet-4-5-20250929' },
    
    // DeepSeek models
    'deepseek_fast': { provider: 'deepseek', model: 'deepseek-chat' },
    'deepseek_quality': { provider: 'deepseek', model: 'deepseek-reasoner' },
  };

  const result = mapping[alias] || null;
  
  return result;
}

