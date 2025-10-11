import React from 'react';
import { useTranslation } from 'react-i18next';

export default function Homepage() {
  const { t } = useTranslation();
  return (
    <div className="min-h-screen bg-white text-slate-900 dark:bg-slate-900 dark:text-slate-100">
      <div className="max-w-5xl mx-auto p-6">
        <h1 className="text-2xl font-semibold">{t('homepage.title')}</h1>
        <p className="mt-2 text-slate-600 dark:text-slate-400">{t('homepage.soon')}</p>
      </div>
    </div>
  );
}

