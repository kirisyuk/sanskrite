import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

const resources = {
  en: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Search…' },
      homepage: { title: 'Homepage', soon: 'Content coming soon' },
      unfold: {
        request: {
          ariaLabel: 'Unfold query',
          placeholders: {
            initial: [
              'Formulate a research question…',
              'Describe the problem you’re investigating…',
              'Outline your approach or method…',
              'Summarize the context or data…',
              'List constraints or success criteria…',
              'What do we want to learn next?',
            ],
            ongoing: [
              'Dig deeper into the last result…',
              'Cross-check a claim or source…',
              'Compare alternative explanations…',
              'Identify gaps or open questions…',
              'Propose the next experiment or step…',
              'Extract key takeaways from above…',
            ],
            begin: 'Shall we begin?',
            continue: 'Continue where we left off…',
            helpToday: 'What can I help you with today?',
          },
          attachTitle: 'Attach document',
          modelStrengthLabel: 'Model strength',
          strengthFast: 'Fast model',
          strengthBalanced: 'Balanced model',
          strengthQuality: 'Best available model',
          webSearch: 'Web search',
          highRiskTag: 'High Risk',
          highRiskConversation: 'High risk conversation',
          settings: 'Settings',
          send: 'Send',
          settingsModal: {
            title: 'GenAI Settings',
            provider: 'Provider',
            strength: 'Strength',
            stanceProvider: 'Stance provider',
            stanceStrength: 'Stance strength',
            stanceModel: 'Stance model (alias)',
            stanceModelHelp: 'Default: gemini_fast. Accepts: gemini_fast/balanced/quality, gpt_fast/balanced/quality, grok_fast/quality.',
            groundingTitle: 'Search Grounding',
            groundingDesc: 'Enable Google Search grounding',
            temperature: 'Temperature',
            maxTokens: 'Max tokens',
            systemInstruction: 'System instruction',
            enableTelegramTool: 'Enable Telegram tool',
            enableTelegramToolDesc: 'Add telegram_fetch_updates to tools',
            done: 'Done',
          },
        },
        actions: {
          askAgent: 'Ask this agent',
          refinePrev: 'Refine the previous answer',
          bringIn: 'Bring in another model',
          copyAnswer: 'Copy answer',
          send: 'Send',
          cancel: 'Cancel',
          retry: 'Retry',
          switchModel: 'Switch model…',
          copyRequest: 'Copy request',
          newChat: 'New Chat',
          close: 'Close',
          user: 'User',
          discussionFlow: 'Discussion Flow',
          artifacts: 'Artifacts',
        },
        titles: {
          askAgent: 'Ask this agent',
          refinePrevExplain: 'Refine the previous primary answer based on this contribution.',
          bringInExplain: 'Bring in another AI model to the discussion.',
          copyAnswer: 'Copy answer',
          hideTitleSuggestions: 'Hide title suggestions',
          showTitleSuggestions: 'Show title suggestions',
          openMetricAnalysis: 'Open metric analysis',
          addSource: 'Add source',
          close: 'Close',
          dismiss: 'Dismiss',
          newChat: 'New Chat',
        },
        status: {
          highRisk: 'High-risk conversation',
          thinking: 'Thinking... {{seconds}}s',
          noContent: 'No content.',
          failed: 'failed',
          applyingEdit: 'Applying edit… {{seconds}}s',
          editFailed: 'Edit failed: {{message}}',
          graphPlaceholder: 'Graph will appear here',
          loadingMetricAnalysis: 'Loading metric analysis…',
        },
        sections: {
          navigation: 'Navigation',
          noChatsYet: 'No chats yet',
          settings: 'Settings',
          adminAccess: 'Admin access',
          yourStance: 'Your stance',
          assistantsStance: "Assistant's stance:",
          discussionFlow: 'Discussion Flow:',
          artifacts: 'Artifacts',
          biasLastMessages: 'Bias (last messages)',
          nameTopic: 'How would you name this topic?',
          youInThisDiscussion: 'You in this discussion',
          noClearPosition: 'No clear position identified.',
          noReviewYet: 'No review yet.',
        },
        artifacts: {
          noArtifactsYet: 'No artifacts yet.',
          entities: 'Entities',
          metrics: 'Metrics',
          unit: 'Unit: {{unit}}',
          frequency: 'Frequency: {{frequency}}',
        },
        sources: {
          addSource: 'Add source',
          telegramChannel: 'Telegram Channel',
          webPage: 'Web Page',
          rss: 'RSS',
          document: 'Document',
          addTelegramChannel: 'Add Telegram Channel',
          telegramPlaceholder: '@channel or t.me/channel',
        },
      },
    },
  },
  ru: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Поиск…' },
      homepage: { title: 'Главная', soon: 'Скоро здесь будет контент' },
    },
  },
  de: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Suchen…' },
      homepage: { title: 'Startseite', soon: 'Inhalt folgt in Kürze' },
    },
  },
  es: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Buscar…' },
      homepage: { title: 'Inicio', soon: 'Contenido próximamente' },
    },
  },
  kk: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Іздеу…' },
      homepage: { title: 'Басты бет', soon: 'Жақында мазмұн пайда болады' },
    },
  },
};

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'en',
    supportedLngs: ['en', 'ru', 'de', 'es', 'kk'],
    interpolation: { escapeValue: false },
    detection: { order: ['querystring', 'localStorage', 'navigator'] },
  });

export default i18n;

