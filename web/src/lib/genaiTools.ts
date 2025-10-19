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


