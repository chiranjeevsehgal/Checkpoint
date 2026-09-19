import { base64Encode } from './base64.ts';

export interface ReminderPush {
  id: string;
  title: string;
  body: string;
  priority?: number;
}

/**
 * Builds the ntfy WebSocket subscribe URL from the server-provided base so a
 * LAN IP, hostname or path-prefixed base all work. http -> ws, https -> wss.
 * A per-user read token is passed via ntfy's ?auth= query parameter.
 */
export function buildWebSocketUrl(ntfyUrl: string, topic: string, token?: string): string {
  const base = ntfyUrl
    .trim()
    .replace(/\/+$/, '')
    .replace(/^http:/i, 'ws:')
    .replace(/^https:/i, 'wss:');
  const url = `${base}/${topic}/ws`;
  return token ? `${url}?auth=${authParam(token)}` : url;
}

/** ntfy wants raw base64 of the Authorization header, padding stripped. */
function authParam(token: string): string {
  const header = `Bearer ${token}`;
  const bytes = new Uint8Array(header.length);
  for (let i = 0; i < header.length; i++) bytes[i] = header.charCodeAt(i) & 0xff;
  return base64Encode(bytes).replace(/=+$/, '');
}

/**
 * Parses an ntfy `/ws` frame into a reminder push. Returns null for anything
 * that is not a message event or has no body (open/keepalive frames).
 */
export function parseReminderPush(raw: string): ReminderPush | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null) return null;

  const event = parsed as Record<string, unknown>;
  if (event.event !== 'message') return null;
  const body = typeof event.message === 'string' ? event.message : '';
  if (!body) return null;

  const title = typeof event.title === 'string' && event.title ? event.title : 'Reminder';
  const id =
    typeof event.id === 'string' && event.id ? event.id : `${String(event.time ?? '')}:${body}`;
  const priority = typeof event.priority === 'number' ? event.priority : undefined;
  return { id, title, body, priority };
}
