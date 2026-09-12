import { UPLOAD_RETRY_DELAYS_MS } from './config.ts';

export function nextRetryDelayMs(attempts: number): number {
  if (attempts <= 0) return UPLOAD_RETRY_DELAYS_MS[0]!;
  const index = Math.min(attempts, UPLOAD_RETRY_DELAYS_MS.length) - 1;
  return UPLOAD_RETRY_DELAYS_MS[index]!;
}
