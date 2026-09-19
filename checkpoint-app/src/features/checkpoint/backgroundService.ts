import notifee, { AndroidForegroundServiceType, AndroidImportance } from '@notifee/react-native';
import { PermissionsAndroid, Platform } from 'react-native';

import { networkMonitor } from './networkMonitor.ts';
import { hasBlePermissions } from './permissions.ts';
import {
  configureReminders,
  loadReminderSubscription,
  startReminders,
} from './reminderSubscriber.ts';
import { loadSettings } from './settings.ts';
import { syncEngine } from './syncEngine.ts';

import { getSessionToken, loadSession } from '@/lib/session';

const CHANNEL_ID = 'checkpoint-sync';
const NOTIFICATION_ID = 'checkpoint-sync';

let serviceActive = false;

export interface SyncStatus {
  connected: boolean;
  recording: boolean;
  reminders: boolean;
}

function syncBody({ connected, recording, reminders }: SyncStatus): string {
  if (connected) return recording ? "I'm all ears" : 'Pendant Mic is off';
  if (reminders) return 'Reminders on';
  return 'Not Connected';
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
  await loadSession();
  if (!getSessionToken()) {
    console.debug('[bg] no session — engine not started');
    return;
  }
  const settings = await loadSettings();
  syncEngine.configure(settings);
  networkMonitor.configure(settings.serverUrl);
  void networkMonitor.start();
  void syncEngine.start();
  const subscription = await loadReminderSubscription();
  if (subscription) {
    configureReminders(subscription);
    startReminders();
  }
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
  // A `connectedDevice` foreground service needs a granted Bluetooth runtime
  // permission, but the reminders data-sync service does not.
  const bleGranted = await hasBlePermissions();
  if (!bleGranted && !status.reminders) {
    console.debug('[bg] service deferred: Bluetooth permission not granted');
    return;
  }
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
    const foregroundServiceTypes = [AndroidForegroundServiceType.FOREGROUND_SERVICE_TYPE_DATA_SYNC];
    if (bleGranted) {
      foregroundServiceTypes.push(
        AndroidForegroundServiceType.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE,
      );
    }
    await notifee.displayNotification({
      id: NOTIFICATION_ID,
      title: 'Checkpoint',
      body: syncBody(status),
      android: {
        channelId: CHANNEL_ID,
        asForegroundService: true,
        ongoing: true,
        foregroundServiceTypes,
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
