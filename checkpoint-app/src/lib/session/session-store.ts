import type { Session } from './types';

import { storage } from '@/lib/storage';
import { sessionKey } from '@/lib/storage/keys';

let current: Session | null = null;

export function getSession(): Session | null {
  return current;
}

export function getSessionToken(): string | null {
  return current?.token ?? null;
}

export function getIdentityId(): string | null {
  return current?.identityId ?? null;
}

export async function loadSession(): Promise<Session | null> {
  const raw = await storage.get(sessionKey());
  current = raw ? parseSession(raw) : null;
  return current;
}

export async function saveSession(session: Session): Promise<void> {
  current = session;
  await storage.set(sessionKey(), JSON.stringify(session));
}

export async function clearSession(): Promise<void> {
  current = null;
  await storage.remove(sessionKey());
}

function parseSession(raw: string): Session | null {
  try {
    const value = JSON.parse(raw) as Partial<Session>;
    if (
      typeof value.token === 'string' &&
      value.token.length > 0 &&
      typeof value.identityId === 'string' &&
      value.identityId.length > 0
    ) {
      return { token: value.token, identityId: value.identityId };
    }
  } catch {
    // fall through
  }
  return null;
}
