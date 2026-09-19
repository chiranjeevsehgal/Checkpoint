import { apiFetch } from './api-client';

export interface NotificationChannel {
  enabled: boolean;
  ntfy_url: string;
  topic?: string;
  token?: string;
  advance_minutes?: number;
}

export async function getNotificationSettings(token: string): Promise<NotificationChannel> {
  return apiFetch<NotificationChannel>('/v1/me/notifications', {}, token);
}

/** Enables reminders, minting a topic on first use; repeat calls keep it. */
export async function enableNotifications(token: string): Promise<NotificationChannel> {
  return apiFetch<NotificationChannel>('/v1/me/notifications', { method: 'POST' }, token);
}

/** Disables reminders and clears (rotates) the topic server-side. */
export async function disableNotifications(token: string): Promise<NotificationChannel> {
  return apiFetch<NotificationChannel>('/v1/me/notifications', { method: 'DELETE' }, token);
}

/** Sets how many minutes before a reminder the advance push is sent. */
export async function updateNotificationAdvance(
  token: string,
  advanceMinutes: number,
): Promise<NotificationChannel> {
  return apiFetch<NotificationChannel>(
    '/v1/me/notifications',
    { method: 'PUT', body: JSON.stringify({ advance_minutes: advanceMinutes }) },
    token,
  );
}
