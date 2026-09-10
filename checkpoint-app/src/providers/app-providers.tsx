import type { PropsWithChildren } from 'react';

import { CheckpointProvider } from '@/features/checkpoint/hooks/useCheckpoint';
import { ThemeProvider } from '@/providers/theme-provider';

export function AppProviders({ children }: PropsWithChildren) {
  return (
    <ThemeProvider>
      <CheckpointProvider>{children}</CheckpointProvider>
    </ThemeProvider>
  );
}
