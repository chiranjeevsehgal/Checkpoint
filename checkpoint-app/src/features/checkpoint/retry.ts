import { UPLOAD_RETRY_DELAYS_MS } from './config.ts';

export function nextRetryDelayMs(attempts: number): number {
  if (attempts <= 0) return UPLOAD_RETRY_DELAYS_MS[0]!;
  const index = Math.min(attempts, UPLOAD_RETRY_DELAYS_MS.length) - 1;
  return UPLOAD_RETRY_DELAYS_MS[index]!;
}

/** Double a reconnect backoff, capped at maxMs. */
export function growBackoffMs(current: number, maxMs: number): number {
  return Math.min(current * 2, maxMs);
}
