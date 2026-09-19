import { apiFetch } from './api-client';

export interface McpKey {
  id: number;
  name: string;
  prefix: string;
  created_at: string;
  last_used_at?: string;
}

export interface CreatedMcpKey extends McpKey {
  /** Returned once on create; the server never exposes it again. */
  key: string;
}

export async function listMcpKeys(token: string): Promise<McpKey[]> {
  const result = await apiFetch<{ keys: McpKey[] }>('/v1/me/mcp-keys', {}, token);
  return result.keys;
}

export async function createMcpKey(token: string, name: string): Promise<CreatedMcpKey> {
  return apiFetch<CreatedMcpKey>(
    '/v1/me/mcp-keys',
    { method: 'POST', body: JSON.stringify({ name }) },
    token,
  );
}

export async function revokeMcpKey(token: string, id: number): Promise<void> {
  await apiFetch<{ revoked: boolean }>(`/v1/me/mcp-keys/${id}`, { method: 'DELETE' }, token);
}
