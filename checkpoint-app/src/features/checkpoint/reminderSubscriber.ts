import notifee, { AndroidImportance } from '@notifee/react-native';
import { Platform } from 'react-native';

import { buildWebSocketUrl, parseReminderPush } from './reminderMessages.ts';

import { storage } from '@/lib/storage';
import { notificationSubscriptionKey } from '@/lib/storage/keys';

export interface ReminderSubscription {
  ntfyUrl: string;
  topic: string;
}

const CHANNEL_ID = 'checkpoint-reminders';
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30_000;
const SEEN_ID_LIMIT = 200;

let subscription: ReminderSubscription | null = null;
let socket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let attempts = 0;
let running = false;
let channelReady = false;
const seenIds = new Set<string>();

export async function loadReminderSubscription(): Promise<ReminderSubscription | null> {
  const raw = await storage.get(notificationSubscriptionKey());
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as ReminderSubscription;
    if (parsed?.ntfyUrl && parsed?.topic) return parsed;
  } catch {
    // A malformed record is treated as no subscription.
  }
  return null;
}

export async function saveReminderSubscription(sub: ReminderSubscription): Promise<void> {
  await storage.set(notificationSubscriptionKey(), JSON.stringify(sub));
}

export async function clearReminderSubscription(): Promise<void> {
  await storage.remove(notificationSubscriptionKey());
}

/** Sets the channel the subscriber connects to. Does not open the socket. */
export function configureReminders(sub: ReminderSubscription | null): void {
  subscription = sub;
}

export function startReminders(): void {
  if (Platform.OS === 'web' || running || !subscription) return;
  running = true;
  attempts = 0;
  void ensureChannel().then(openSocket);
}

export function stopReminders(): void {
  running = false;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (socket) {
    try {
      socket.close(1000);
    } catch {
      // Ignore close failures during teardown.
    }
    socket = null;
  }
}

async function ensureChannel(): Promise<void> {
  if (channelReady || Platform.OS !== 'android') return;
  await notifee.createChannel({
    id: CHANNEL_ID,
    name: 'Reminders',
    importance: AndroidImportance.HIGH,
  });
  channelReady = true;
}

function openSocket(): void {
  if (!running || !subscription) return;
  const url = buildWebSocketUrl(subscription.ntfyUrl, subscription.topic);

  let ws: WebSocket;
  try {
    ws = new WebSocket(url);
  } catch (error) {
    console.warn(`[reminders] open failed: ${error instanceof Error ? error.message : 'unknown'}`);
    scheduleReconnect();
    return;
  }

  socket = ws;
  ws.onopen = () => {
    attempts = 0;
  };
  ws.onmessage = (event) => {
    void showPush(event.data);
  };
  ws.onclose = () => {
    if (socket === ws) socket = null;
    scheduleReconnect();
  };
}

function scheduleReconnect(): void {
  if (!running || reconnectTimer) return;
  const delay = Math.min(RECONNECT_BASE_MS * 2 ** attempts, RECONNECT_MAX_MS);
  attempts += 1;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    openSocket();
  }, delay);
}

async function showPush(raw: unknown): Promise<void> {
  if (typeof raw !== 'string') return;
  const push = parseReminderPush(raw);
  if (!push || seenIds.has(push.id)) return;
  rememberId(push.id);
  try {
    await notifee.displayNotification({
      id: push.id,
      title: push.title,
      body: push.body,
      android: {
        channelId: CHANNEL_ID,
        pressAction: { id: 'default' },
      },
    });
  } catch (error) {
    console.warn(
      `[reminders] display failed: ${error instanceof Error ? error.message : 'unknown'}`,
    );
  }
}

/**
 * ntfy may replay cached frames on reconnect; remember recent message ids so a
 * reconnect never duplicates a notification.
 */
function rememberId(id: string): void {
  if (seenIds.size >= SEEN_ID_LIMIT) seenIds.clear();
  seenIds.add(id);
}
