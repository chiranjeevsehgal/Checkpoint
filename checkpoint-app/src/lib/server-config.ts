import { apiUrlForHost, kratosUrlForHost, normalizeHost } from './server-urls';

import { env } from '@/lib/env';
import { storage } from '@/lib/storage';
import { prefKey } from '@/lib/storage/keys';

const HOST_KEY = prefKey('devServerHost');

let devHost: string | null = null;
let loadPromise: Promise<void> | null = null;
const listeners = new Set<() => void>();

export function getDevHost(): string | null {
  return devHost;
}

export function subscribeServerConfig(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function emit(): void {
  for (const listener of listeners) listener();
}

/** Reads the persisted dev host once, so callers can await it before first use. */
export function loadServerConfig(): Promise<void> {
  loadPromise ??= storage.get(HOST_KEY).then((raw) => {
    devHost = normalizeHost(raw);
  });
  return loadPromise;
}

/** Persists the dev host. An empty value clears the override. */
export async function setDevHost(input: string): Promise<void> {
  devHost = normalizeHost(input);
  await storage.set(HOST_KEY, devHost ?? '');
  emit();
}

export function resolveApiUrl(): string {
  return devHost ? apiUrlForHost(devHost) : env.apiUrl;
}

export function resolveKratosUrl(): string {
  return devHost ? kratosUrlForHost(devHost) : env.kratosUrl;
}
