import { useEffect, type PropsWithChildren } from 'react';

import { useAuth } from '@/features/auth/hooks/useAuth';
import { stopSyncService } from '@/features/checkpoint/backgroundService';
import { getDevice } from '@/features/checkpoint/device';
import { useCheckpoint } from '@/features/checkpoint/hooks/useCheckpoint';
import { syncEngine } from '@/features/checkpoint/syncEngine';
import { getSessionToken } from '@/lib/session';

/** Ties the auth state to the checkpoint sync engine: start when signed in
 * with an owned pendant, stop on sign-out, deletion or outage. */
export function AuthSyncBridge({ children }: PropsWithChildren) {
  const { status, reauthenticate } = useAuth();
  const { settings } = useCheckpoint();

  useEffect(() => {
    syncEngine.setReauthenticator(status === 'authenticated' ? reauthenticate : null);
  }, [status, reauthenticate]);

  useEffect(() => {
    if (status === 'loading') return;
    if (status !== 'authenticated') {
      if (status !== 'unavailable') {
        syncEngine.setOwnedDevice(null);
        void syncEngine.stopForAuthLoss();
        void stopSyncService();
        if (status === 'anonymous' || status === 'deleting') {
          syncEngine.clearLocalData();
        }
      }
      return;
    }

    let cancelled = false;
    void (async () => {
      const token = getSessionToken();
      if (!token) return;
      try {
        const device = await getDevice(settings.serverUrl, token);
        if (cancelled) return;
        syncEngine.setOwnedDevice(device?.device_id ?? null);
      } catch {
        if (cancelled) return;
        syncEngine.setOwnedDevice(null);
      }
      if (!cancelled) void syncEngine.start();
    })();

    return () => {
      cancelled = true;
    };
  }, [status, settings.serverUrl]);

  return <>{children}</>;
}
