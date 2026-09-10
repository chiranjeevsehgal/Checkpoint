import { env } from '@/lib/env';

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function apiFetch<T>(
  path: string,
  init?: RequestInit,
  token?: string
): Promise<T> {
  const res = await fetch(`${env.apiUrl}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init?.headers,
    },
    ...init,
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new ApiError(res.status, `Request failed: ${res.status} ${body.slice(0, 300)}`);
  }
  return (await res.json()) as T;
}

export async function apiPutBytes(
  url: string,
  bytes: Uint8Array,
  contentType: string
): Promise<number> {
  const res = await fetch(url, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: bytes as unknown as BodyInit,
  });
  if (res.status !== 200 && res.status !== 201 && res.status !== 204) {
    const body = await res.text().catch(() => '');
    throw new ApiError(res.status, `PUT presigned -> HTTP ${res.status} ${body.slice(0, 300)}`);
  }
  return res.status;
}
