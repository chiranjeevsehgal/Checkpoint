import { useColorScheme } from 'nativewind';
import * as React from 'react';

export function ThemeProvider({ children }: React.PropsWithChildren) {
  const { colorScheme } = useColorScheme();
  void colorScheme;
  return children;
}

export function useAppTheme() {
  const { colorScheme, setColorScheme, toggleColorScheme } = useColorScheme();
  return { colorScheme: colorScheme ?? 'light', setColorScheme, toggleColorScheme };
}
