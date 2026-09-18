import { useEffect, type PropsWithChildren } from 'react';
import { AppState } from 'react-native';

import { useAuth } from '@/features/auth/hooks/useAuth';
import { stopSyncService } from '@/features/checkpoint/backgroundService';
import { getDevice } from '@/features/checkpoint/device';
import { useCheckpoint } from '@/features/checkpoint/hooks/useCheckpoint';
import { syncEngine } from '@/features/checkpoint/syncEngine';
import { putUserSettings } from '@/lib/api/settings-api';
import { getDeviceTimeZone } from '@/lib/device-timezone';
import { getSessionToken } from '@/lib/session';

let lastSynced: { token: string; timezone: string } | null = null;

/** Pushes the device timezone to the user's settings when it changed. */
async function syncDeviceTimeZone(): Promise<void> {
  const token = getSessionToken();
  const timezone = getDeviceTimeZone();
  if (!token || !timezone) return;
  if (lastSynced?.token === token && lastSynced.timezone === timezone) return;
  lastSynced = { token, timezone };
  try {
    await putUserSettings(token, { timezone });
  } catch {
    lastSynced = null;
  }
}

/** Ties the auth state to the checkpoint sync engine: start when signed in
 * with an owned pendant, stop on sign-out, deletion or outage. */
export function AuthSyncBridge({ children }: PropsWithChildren) {
  const { status, reauthenticate } = useAuth();
  const { settings } = useCheckpoint();

  useEffect(() => {
    syncEngine.setReauthenticator(status === 'authenticated' ? reauthenticate : null);
  }, [status, reauthenticate]);

  useEffect(() => {
    if (status !== 'authenticated') return;
    void syncDeviceTimeZone();
    const subscription = AppState.addEventListener('change', (state) => {
      if (state === 'active') void syncDeviceTimeZone();
    });
    return () => subscription.remove();
  }, [status]);

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
