import { apiFetch } from './api-client';

export async function deleteAccount(token: string): Promise<void> {
  await apiFetch<{ status: string }>('/v1/me', { method: 'DELETE' }, token);
}
