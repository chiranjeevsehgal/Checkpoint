// Stored agent token (session scope: never persisted to disk).
const TOKEN_KEY = 'ck-admin-token';

export function getAgentToken(): string | null {
  if (typeof sessionStorage === 'undefined') return null;
  return sessionStorage.getItem(TOKEN_KEY);
}

export function setAgentToken(token: string): void {
  if (typeof sessionStorage === 'undefined') return;
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
}

// EventSource cannot send headers, so the token travels as a query parameter
// on /events (accepted server-side only there).
export function eventsUrl(channel: string): string {
  const token = getAgentToken();
  const params = new URLSearchParams({ channel });
  if (token) params.set('access_token', token);
  return `/events?${params.toString()}`;
}
