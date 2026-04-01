import React from 'react';
import { useTranslation } from 'react-i18next';
import darkLogo from '../assets/nv-horizon-dark-theme.png';

type Props = { error?: Error | null; onRetry?: () => void };

export default function StartupError({ error, onRetry }: Props) {
  const { t } = useTranslation();
  return (
    <div data-theme="dark" className="min-h-screen flex flex-col items-center justify-center p-6">
      <img src={darkLogo} alt="horizon.nordicvision.io" className="h-4 w-auto mb-4" />
      <div className="w-full max-w-xl text-center">
        <h1 className="text-xl font-semibold mb-2">{t('unfold.startup.temporaryIssue')}</h1>
        <p className="text-slate-300 mb-4">
          {t('unfold.startup.reloadMessage')}
        </p>
        <br />
        {error && (
          <pre className="text-xs text-red-400 bg-red-950/30 rounded-md p-3 overflow-auto mb-4">
            {error.message}
          </pre>
        )}
        <div className="flex items-center justify-center gap-3">
          <button
            className="btn btn-secondary"
            onClick={() => (onRetry ? onRetry() : window.location.reload())}
          >
            {t('unfold.startup.retry')}
          </button>
        </div>
      </div>
    </div>
  );
}

