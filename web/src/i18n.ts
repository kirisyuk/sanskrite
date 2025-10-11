import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

const resources = {
  en: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Search…' },
      homepage: { title: 'Homepage', soon: 'Content coming soon' },
    },
  },
  ru: {
    translation: {
      app: { title: 'Sanskrite' },
      search: { placeholder: 'Поиск…' },
      homepage: { title: 'Главная', soon: 'Скоро здесь будет контент' },
    },
  },
};

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'en',
    supportedLngs: ['en', 'ru'],
    interpolation: { escapeValue: false },
    detection: { order: ['querystring', 'localStorage', 'navigator'] },
  });

export default i18n;

