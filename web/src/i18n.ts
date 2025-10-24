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

