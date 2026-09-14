import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  hasVerifiedEmail,
  identityEmail,
  submitLogin,
  whoami,
  type KratosTransport,
} from '../kratos-client.ts';

describe('kratos helpers', () => {
  it('detects a completed email verification', () => {
    assert.ok(
      hasVerifiedEmail({
        id: 'x',
        verifiable_addresses: [{ via: 'email', verified: true, status: 'completed' }],
      }),
    );
    assert.ok(
      !hasVerifiedEmail({
        id: 'x',
        verifiable_addresses: [{ via: 'email', verified: false, status: 'sent' }],
      }),
    );
    assert.ok(!hasVerifiedEmail(undefined));
  });

  it('reads the email trait', () => {
    assert.equal(identityEmail({ id: 'x', traits: { email: 'a@b.c' } }), 'a@b.c');
    assert.equal(identityEmail(undefined), null);
  });
});

describe('kratos flow submissions', () => {
  it('submits login with identifier and password', async () => {
    const calls: { method: string; path: string; body: unknown }[] = [];
    const transport: KratosTransport = {
      request(method, path, body) {
        calls.push({ method, path, body });
        return Promise.resolve({ session_token: 't' } as never);
      },
    };
    await submitLogin(
      transport,
      { id: 'f', ui: { action: '/self-service/login?flow=f', method: 'POST' } },
      'a@b.c',
      'pw',
    );
    assert.equal(calls.length, 1);
    assert.equal(calls[0]?.method, 'POST');
    assert.deepEqual(calls[0]?.body, { method: 'password', identifier: 'a@b.c', password: 'pw' });
  });

  it('whoami forwards the session token', async () => {
    let seen: string | undefined;
    const transport: KratosTransport = {
      request(_method, _path, _body, token) {
        seen = token;
        return Promise.resolve({ active: true } as never);
      },
    };
    await whoami(transport, 'sess');
    assert.equal(seen, 'sess');
  });
});
