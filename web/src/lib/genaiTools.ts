export type JsonSchema = Record<string, any>;

export interface ToolDeclaration {
  name: string;
  description?: string;
  parameters: JsonSchema;
}

// Telegram fetch tool declaration (mirrors backend NewTelegramFetchToolDeclaration)
export const TelegramFetchUpdatesTool: ToolDeclaration = {
  name: 'telegram_fetch_updates',
  description: 'Fetch recent messages from a public Telegram channel by username',
  parameters: {
    type: 'object',
    properties: {
      channel: { type: 'string', description: 'Channel reference: @name or t.me/name' },
      lastId: { type: 'integer', minimum: 0 },
      limit: { type: 'integer', minimum: 1, maximum: 100 },
    },
    required: ['channel'],
  },
};

// Web search tool declaration (mirrors backend web_search tool)
export const WebSearchTool: ToolDeclaration = {
  name: 'web_search',
  description: 'Search the web for current information. Use this tool when you need to find up-to-date information from the internet, check current events, look up facts, or find recent news. This is a separate web search tool, not related to search grounding or other built-in search capabilities.',
  parameters: {
    type: 'object',
    properties: {
      query: {
        type: 'string',
        description: 'Search query in natural language (e.g., \'latest news about AI\', \'current weather in Moscow\', \'recent developments in quantum computing\')',
      },
      limit: {
        type: 'integer',
        description: 'Maximum number of results to return (1-20, default: 10)',
        minimum: 1,
        maximum: 20,
      },
    },
    required: ['query'],
  },
};


