import type { PropsWithChildren } from 'react';

import { CheckpointProvider } from '@/features/checkpoint/hooks/useCheckpoint';
import { ThemeProvider } from '@/providers/theme-provider';
import { ToastProvider } from '@/providers/toast-provider';

export function AppProviders({ children }: PropsWithChildren) {
  return (
    <ThemeProvider>
      <ToastProvider>
        <CheckpointProvider>{children}</CheckpointProvider>
      </ToastProvider>
    </ThemeProvider>
  );
}
