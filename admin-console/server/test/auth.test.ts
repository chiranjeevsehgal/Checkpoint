import { describe, expect, it, vi } from 'vitest';

import type { FastifyReply, FastifyRequest } from 'fastify';

import { assertBindAllowed, isLoopback, requireApiAuth } from '../src/auth';

function request(url: string, authorization?: string): FastifyRequest {
  return {
    url,
    headers: authorization ? { authorization } : {},
    query: {},
  } as FastifyRequest;
}

function reply(): { reply: FastifyReply; sent: { code?: number; body?: unknown } } {
  const sent: { code?: number; body?: unknown } = {};
  const rep = {
    code: (code: number) => {
      sent.code = code;
      return rep;
    },
    send: (body: unknown) => {
      sent.body = body;
      return rep;
    },
  } as unknown as FastifyReply;
  return { reply: rep, sent };
}

describe('bind guard', () => {
  it('treats loopback hosts as local', () => {
    expect(isLoopback('127.0.0.1')).toBe(true);
    expect(isLoopback('::1')).toBe(true);
    expect(isLoopback('localhost')).toBe(true);
    expect(isLoopback('0.0.0.0')).toBe(false);
  });

  it('refuses a public bind without a token', () => {
    vi.stubEnv('ADMIN_TOKEN', '');
    expect(() => assertBindAllowed('0.0.0.0')).toThrow();
    expect(() => assertBindAllowed('127.0.0.1')).not.toThrow();
    vi.unstubAllEnvs();
  });

  it('allows a public bind with a token', () => {
    vi.stubEnv('ADMIN_TOKEN', 'secret');
    expect(() => assertBindAllowed('0.0.0.0')).not.toThrow();
    vi.unstubAllEnvs();
  });
});

describe('api auth hook', () => {
  it('lets everything through without a token', async () => {
    vi.stubEnv('ADMIN_TOKEN', '');
    const { reply: rep, sent } = reply();
    await requireApiAuth(request('/api/devices'), rep);
    expect(sent.code).toBeUndefined();
    vi.unstubAllEnvs();
  });

  it('leaves the health probe open with a token', async () => {
    vi.stubEnv('ADMIN_TOKEN', 'secret');
    const { reply: rep, sent } = reply();
    await requireApiAuth(request('/api/health'), rep);
    expect(sent.code).toBeUndefined();
    vi.unstubAllEnvs();
  });

  it('rejects api calls with a wrong token', async () => {
    vi.stubEnv('ADMIN_TOKEN', 'secret');
    const { reply: rep, sent } = reply();
    await requireApiAuth(request('/api/devices', 'Bearer wrong'), rep);
    expect(sent.code).toBe(401);
    vi.unstubAllEnvs();
  });

  it('accepts api calls with the right token', async () => {
    vi.stubEnv('ADMIN_TOKEN', 'secret');
    const { reply: rep, sent } = reply();
    await requireApiAuth(request('/api/devices', 'Bearer secret'), rep);
    expect(sent.code).toBeUndefined();
    vi.unstubAllEnvs();
  });

  it('accepts the events token as a query parameter', async () => {
    vi.stubEnv('ADMIN_TOKEN', 'secret');
    const { reply: rep, sent } = reply();
    const req = request('/events?channel=tasks') as FastifyRequest & {
      query: { access_token: string };
    };
    req.query = { access_token: 'secret' };
    await requireApiAuth(req, rep);
    expect(sent.code).toBeUndefined();
    vi.unstubAllEnvs();
  });
});
