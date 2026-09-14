import { fetchWithTimeout } from '@/lib/http';
import { resolveApiUrl } from '@/lib/server-config';

export class ApiError extends Error {
  status: number;
  code?: string;

  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

function errorCode(body: string): string | undefined {
  try {
    const parsed = JSON.parse(body) as { error?: { code?: unknown } };
    return typeof parsed.error?.code === 'string' ? parsed.error.code : undefined;
  } catch {
    return undefined;
  }
}

export async function apiFetch<T>(path: string, init?: RequestInit, token?: string): Promise<T> {
  const url = path.startsWith('http') ? path : `${resolveApiUrl()}${path}`;
  const { headers: initHeaders, ...restInit } = init ?? {};
  const res = await fetchWithTimeout(url, {
    ...restInit,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(initHeaders as Record<string, string> | undefined),
    },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new ApiError(
      res.status,
      `Request failed: ${res.status} ${body.slice(0, 300)}`,
      errorCode(body),
    );
  }
  return (await res.json()) as T;
}

export async function apiPutBytes(
  url: string,
  bytes: Uint8Array,
  contentType: string,
  signal?: AbortSignal,
): Promise<number> {
  const res = await fetch(url, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: bytes as unknown as BodyInit,
    signal,
  });
  if (res.status !== 200 && res.status !== 201 && res.status !== 204) {
    const body = await res.text().catch(() => '');
    throw new ApiError(res.status, `PUT presigned -> HTTP ${res.status} ${body.slice(0, 300)}`);
  }
  return res.status;
}
