import { timingSafeEqual } from 'node:crypto';

import type { FastifyReply, FastifyRequest } from 'fastify';

// Bearer-token gate for the local agent. When ADMIN_TOKEN is unset the agent
// keeps its historical localhost-only posture (and refuses a non-loopback
// bind, see assertBindAllowed). When set, every /api and /events route
// requires it; /api/health stays open for container probes.
export function adminToken(): string {
  return (process.env.ADMIN_TOKEN ?? '').trim();
}

export function isLoopback(host: string): boolean {
  const normalized = host.trim().toLowerCase().replace(/[\[\]]/g, '');
  return normalized === '127.0.0.1' || normalized === '::1' || normalized === 'localhost';
}

export function assertBindAllowed(host: string): void {
  if (!isLoopback(host) && !adminToken()) {
    throw new Error('Refusing to bind a non-loopback host without ADMIN_TOKEN set.');
  }
}

function safeEqual(presented: string, expected: string): boolean {
  const a = Buffer.from(presented);
  const b = Buffer.from(expected);
  return a.length === b.length && timingSafeEqual(a, b);
}

export function bearerValid(request: FastifyRequest): boolean {
  const token = adminToken();
  if (!token) return true;
  const header = (request.headers.authorization ?? '').trim();
  const presented = /^bearer\s+/i.test(header) ? header.replace(/^bearer\s+/i, '').trim() : '';
  if (presented && safeEqual(presented, token)) return true;
  // EventSource cannot send headers, so /events also accepts the token as a
  // query parameter. Never accepted on /api routes.
  if (request.url.startsWith('/events')) {
    const query = (request.query as { access_token?: unknown }).access_token;
    if (typeof query === 'string' && query && safeEqual(query, token)) return true;
  }
  return false;
}

export async function requireApiAuth(request: FastifyRequest, reply: FastifyReply): Promise<void> {
  const url = request.url.split(/[?#]/)[0];
  if (url === '/api/health') return;
  if (url.startsWith('/api') || url.startsWith('/events')) {
    if (!bearerValid(request)) {
      await reply.code(401).send({ error: 'unauthorized' });
    }
  }
}
