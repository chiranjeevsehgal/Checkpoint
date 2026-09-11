import { useColorScheme } from 'nativewind';
import * as React from 'react';

import { storage } from '@/lib/storage';
import { prefKey } from '@/lib/storage/keys';

export type ThemeChoice = 'light' | 'dark' | 'system';

const THEME_KEY = prefKey('theme');

function isThemeChoice(value: string | null): value is ThemeChoice {
  return value === 'light' || value === 'dark' || value === 'system';
}

export function ThemeProvider({ children }: React.PropsWithChildren) {
  const { setColorScheme } = useColorScheme();

  React.useEffect(() => {
    let active = true;
    void storage.get(THEME_KEY).then((saved) => {
      if (active && isThemeChoice(saved)) setColorScheme(saved);
    });
    return () => {
      active = false;
    };
  }, [setColorScheme]);

  return children;
}

export function useAppTheme() {
  const { colorScheme, setColorScheme } = useColorScheme();
  const scheme = colorScheme === 'dark' ? 'dark' : 'light';

  const setTheme = React.useCallback(
    (choice: ThemeChoice) => {
      setColorScheme(choice);
      void storage.set(THEME_KEY, choice);
    },
    [setColorScheme],
  );

  const toggleTheme = React.useCallback(() => {
    setTheme(scheme === 'dark' ? 'light' : 'dark');
  }, [scheme, setTheme]);

  return { scheme, setTheme, toggleTheme };
}
