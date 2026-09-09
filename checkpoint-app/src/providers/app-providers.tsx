import type { PropsWithChildren } from 'react';

import { ThemeProvider } from '@/providers/theme-provider';

export function AppProviders({ children }: PropsWithChildren) {
  return <ThemeProvider>{children}</ThemeProvider>;
}
