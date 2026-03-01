import React from 'react';
import { useTranslation } from 'react-i18next';
import lightLogo from '../assets/etigram-light-theme.png';
import darkLogo from '../assets/etigram-dark-theme.png';
import { ThemeContext } from './ThemeProvider';

type Props = { onSubmit: (query: string) => void };

export default function Splash({ onSubmit }: Props) {
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  // Ensure theme has an initial value saved if none existed before
  React.useEffect(() => {
    try {
      const saved = localStorage.getItem('etg.theme');
      if (!saved) {
        localStorage.setItem('etg.theme', theme);
      }
    } catch {}
  }, [theme]);
  const [q, setQ] = React.useState('');

  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-white text-slate-900 dark:bg-slate-900 dark:text-slate-100 p-6">
      <img src={theme === 'dark' ? darkLogo : lightLogo} alt="Etigram" className="h-3 w-auto mb-6" />
      <form
        className="w-full max-w-xl"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit(q.trim());
        }}
      >
        <input
          className="w-full rounded-xl border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 px-4 py-3 outline-none focus:ring-2 focus:ring-indigo-500"
          placeholder={t('search.placeholder')}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </form>
    </div>
  );
}
