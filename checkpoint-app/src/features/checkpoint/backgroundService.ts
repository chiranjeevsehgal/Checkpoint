import notifee, { AndroidForegroundServiceType, AndroidImportance } from '@notifee/react-native';
import { PermissionsAndroid, Platform } from 'react-native';

import { networkMonitor } from './networkMonitor.ts';
import { loadSettings } from './settings.ts';
import { syncEngine } from './syncEngine.ts';

const CHANNEL_ID = 'checkpoint-sync';
const NOTIFICATION_ID = 'checkpoint-sync';

let serviceActive = false;

export interface SyncStatus {
  connected: boolean;
  recording: boolean;
}

function syncBody({ connected, recording }: SyncStatus): string {
  if (!connected) return 'Not Connected';
  return recording ? "I'm all ears" : 'Pendant Mic is off';
}

if (Platform.OS === 'android') {
  notifee.registerForegroundService(() => {
    console.debug('[bg] fgs runner invoked');
    void bootBackgroundSync();
    return new Promise<void>(() => {});
  });
}

async function bootBackgroundSync(): Promise<void> {
  console.debug('[bg] booting engine');
  const settings = await loadSettings();
  syncEngine.configure(settings);
  networkMonitor.configure(settings.serverUrl);
  void networkMonitor.start();
  void syncEngine.start();
  console.debug('[bg] engine booted');
}

async function requestNotificationPermission(): Promise<void> {
  if (Platform.OS !== 'android' || Number(Platform.Version) < 33) return;
  try {
    await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS);
  } catch {
    /* notification permission is best-effort */
  }
}

export async function startSyncService(status: SyncStatus): Promise<void> {
  if (Platform.OS !== 'android') return;
  try {
    if (!serviceActive) {
      console.debug('[bg] starting service');
      await requestNotificationPermission();
      await notifee.createChannel({
        id: CHANNEL_ID,
        name: 'Background sync',
        importance: AndroidImportance.LOW,
      });
      serviceActive = true;
    }
    await notifee.displayNotification({
      id: NOTIFICATION_ID,
      title: 'Checkpoint',
      body: syncBody(status),
      android: {
        channelId: CHANNEL_ID,
        asForegroundService: true,
        ongoing: true,
        foregroundServiceTypes: [
          AndroidForegroundServiceType.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE,
        ],
        pressAction: { id: 'default' },
      },
    });
    console.debug('[bg] service updated');
  } catch (error) {
    console.warn(
      `[bg] start service failed: ${error instanceof Error ? error.message : 'unknown'}`,
    );
  }
}

export async function stopSyncService(): Promise<void> {
  if (Platform.OS !== 'android') return;
  try {
    console.debug('[bg] stopping service');
    serviceActive = false;
    await notifee.stopForegroundService();
    console.debug('[bg] service stopped');
  } catch (error) {
    console.warn(`[bg] stop service failed: ${error instanceof Error ? error.message : 'unknown'}`);
  }
}
