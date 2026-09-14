import type { PropsWithChildren } from 'react';

import { AuthProvider } from '@/features/auth/hooks/useAuth';
import { CheckpointProvider } from '@/features/checkpoint/hooks/useCheckpoint';
import { ThemeProvider } from '@/providers/theme-provider';
import { ToastProvider } from '@/providers/toast-provider';

export function AppProviders({ children }: PropsWithChildren) {
  return (
    <ThemeProvider>
      <ToastProvider>
        <AuthProvider>
          <CheckpointProvider>{children}</CheckpointProvider>
        </AuthProvider>
      </ToastProvider>
    </ThemeProvider>
  );
}
