import React from 'react';

type Theme = 'light' | 'dark';

function getSystemTheme(): Theme {
  if (typeof window === 'undefined') return 'light';
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

export const ThemeContext = React.createContext<{ theme: Theme; setTheme: (t: Theme) => void }>({
  theme: 'dark',
  setTheme: () => {},
});

export const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [theme, setTheme] = React.useState<Theme>(() => {
    // Read from localStorage with key 'etg.theme' to match old frontend
    const saved = typeof localStorage !== 'undefined' ? (localStorage.getItem('etg.theme') as Theme | null) : null;
    // Default to dark theme when nothing is saved
    return saved ?? 'dark';
  });

  React.useEffect(() => {
    const root = document.documentElement;
    // Use data-theme attribute instead of class
    root.setAttribute('data-theme', theme);
    localStorage.setItem('etg.theme', theme);
  }, [theme]);

  return <ThemeContext.Provider value={{ theme, setTheme }}>{children}</ThemeContext.Provider>;
};
