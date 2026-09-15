import type { Identity } from './models';

export function formatTimestamp(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function shortId(value: string, size = 10): string {
  return value.length > size ? `${value.slice(0, size)}…` : value;
}

export function emailStatus(identity: Identity): { email: string; verified: boolean } {
  const email = identity.traits?.email ?? '—';
  const address = identity.verifiable_addresses?.find((entry) => entry.value === email);
  return { email, verified: address?.verified ?? false };
}
