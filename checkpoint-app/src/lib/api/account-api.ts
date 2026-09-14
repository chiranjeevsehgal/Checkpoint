import { apiFetch } from './api-client';

export async function deleteAccount(token: string): Promise<void> {
  await apiFetch<{ status: string }>('/v1/me', { method: 'DELETE' }, token);
}

export async function revokeSessions(token: string): Promise<void> {
  await apiFetch<{ status: string }>('/v1/me/sessions', { method: 'DELETE' }, token);
}
