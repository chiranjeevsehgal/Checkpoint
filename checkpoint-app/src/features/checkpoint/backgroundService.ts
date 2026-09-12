import notifee, { AndroidForegroundServiceType, AndroidImportance } from '@notifee/react-native';
import { PermissionsAndroid, Platform } from 'react-native';

const CHANNEL_ID = 'checkpoint-sync';

if (Platform.OS === 'android') {
  notifee.registerForegroundService(() => new Promise<void>(() => {}));
}

async function requestNotificationPermission(): Promise<void> {
  if (Platform.OS !== 'android' || Number(Platform.Version) < 33) return;
  try {
    await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS);
  } catch {
    /* notification permission is best-effort */
  }
}

export async function startSyncService(): Promise<void> {
  if (Platform.OS !== 'android') return;
  try {
    await requestNotificationPermission();
    await notifee.createChannel({
      id: CHANNEL_ID,
      name: 'Background sync',
      importance: AndroidImportance.LOW,
    });
    await notifee.displayNotification({
      title: 'Checkpoint',
      body: 'Syncing with your pendant',
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
  } catch (error) {
    console.warn(
      `[bg] start service failed: ${error instanceof Error ? error.message : 'unknown'}`,
    );
  }
}

export async function stopSyncService(): Promise<void> {
  if (Platform.OS !== 'android') return;
  try {
    await notifee.stopForegroundService();
  } catch (error) {
    console.warn(`[bg] stop service failed: ${error instanceof Error ? error.message : 'unknown'}`);
  }
}
