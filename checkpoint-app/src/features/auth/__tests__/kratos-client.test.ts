import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  hasVerifiedEmail,
  identityEmail,
  identityName,
  logout,
  messagesFrom,
  recoverySessionToken,
  resolveRequestUrl,
  submitLogin,
  submitPasswordChange,
  submitRegistration,
  submitVerificationEmail,
  verificationFlowFromContinueWith,
  whoami,
  type KratosTransport,
} from '../kratos-client.ts';

describe('resolveRequestUrl', () => {
  it('appends relative paths to the transport origin', () => {
    assert.equal(
      resolveRequestUrl('http://192.168.1.5:4433', '/sessions/whoami'),
      'http://192.168.1.5:4433/sessions/whoami',
    );
  });

  it('re-points absolute flow actions at the transport origin', () => {
    assert.equal(
      resolveRequestUrl(
        'http://192.168.1.5:4433',
        'http://localhost:4433/self-service/registration?flow=abc',
      ),
      'http://192.168.1.5:4433/self-service/registration?flow=abc',
    );
  });
});

describe('messagesFrom', () => {
  it('reads node-scoped Kratos messages', () => {
    const body = {
      ui: {
        nodes: [
          { messages: [{ text: 'The new password must be different from the old password.' }] },
        ],
      },
    };
    assert.deepEqual(messagesFrom(body), [
      'The new password must be different from the old password.',
    ]);
  });

  it('reads the generic error reason', () => {
    assert.deepEqual(messagesFrom({ error: { reason: 'boom' } }), ['boom']);
  });

  it('deduplicates repeated messages', () => {
    const body = {
      ui: {
        messages: [{ text: 'same' }],
        nodes: [{ messages: [{ text: 'same' }] }],
      },
    };
    assert.deepEqual(messagesFrom(body), ['same']);
  });

  it('returns an empty list for an unknown body', () => {
    assert.deepEqual(messagesFrom(undefined), []);
    assert.deepEqual(messagesFrom({}), []);
  });
});

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

  it('reads the name trait', () => {
    assert.equal(identityName({ id: 'x', traits: { name: 'Jane' } }), 'Jane');
    assert.equal(identityName({ id: 'x', traits: { email: 'a@b.c' } }), null);
    assert.equal(identityName(undefined), null);
  });
});

describe('recoverySessionToken', () => {
  it('prefers a top-level session token', () => {
    assert.equal(recoverySessionToken({ session_token: 'top' }), 'top');
  });

  it('reads the token from continue_with', () => {
    assert.equal(
      recoverySessionToken({
        continue_with: [
          { action: 'show_settings_ui', flow: { id: 'settings-flow' } },
          { action: 'set_ory_session_token', ory_session_token: 'recovered' },
        ],
      }),
      'recovered',
    );
  });

  it('returns undefined when no token is present', () => {
    assert.equal(recoverySessionToken({ continue_with: [] }), undefined);
  });
});

describe('verificationFlowFromContinueWith', () => {
  it('rebuilds the verification flow exposed by registration', () => {
    const flow = verificationFlowFromContinueWith({
      continue_with: [
        { action: 'show_verification_ui', flow: { id: 'vf-1' } },
        { action: 'set_ory_session_token', ory_session_token: 't' },
      ],
    });
    assert.deepEqual(flow, {
      id: 'vf-1',
      ui: { action: '/self-service/verification?flow=vf-1', method: 'POST' },
    });
  });

  it('returns null when no verification flow follows', () => {
    assert.equal(verificationFlowFromContinueWith({ continue_with: [] }), null);
    assert.equal(verificationFlowFromContinueWith({}), null);
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

  it('submits registration with the email and name traits', async () => {
    const calls: { method: string; path: string; body: unknown }[] = [];
    const transport: KratosTransport = {
      request(method, path, body) {
        calls.push({ method, path, body });
        return Promise.resolve({} as never);
      },
    };
    await submitRegistration(
      transport,
      { id: 'f', ui: { action: '/self-service/registration?flow=f', method: 'POST' } },
      { email: 'a@b.c', name: 'Jane' },
      'pw',
    );
    assert.deepEqual(calls[0]?.body, {
      method: 'password',
      traits: { email: 'a@b.c', name: 'Jane' },
      password: 'pw',
    });
  });

  it('changes the password without sending traits', async () => {
    const calls: { method: string; token: string | undefined; body: unknown }[] = [];
    const transport: KratosTransport = {
      request(method, path, body, token) {
        calls.push({ method, token, body });
        return Promise.resolve(undefined as never);
      },
    };
    await submitPasswordChange(
      transport,
      { id: 'f', ui: { action: '/self-service/settings?flow=f', method: 'POST' } },
      { password: 'new', token: 'sess' },
    );
    assert.deepEqual(calls[0], {
      method: 'POST',
      token: 'sess',
      body: { method: 'password', password: 'new' },
    });
  });

  it('submits verification email with a transient payload', async () => {
    const calls: { body: unknown }[] = [];
    const transport: KratosTransport = {
      request(_method, _path, body) {
        calls.push({ body });
        return Promise.resolve({} as never);
      },
    };
    await submitVerificationEmail(
      transport,
      { id: 'f', ui: { action: '/self-service/verification?flow=f', method: 'POST' } },
      'a@b.c',
      { context: 'account_deletion' },
    );
    assert.deepEqual(calls[0]?.body, {
      method: 'code',
      email: 'a@b.c',
      transient_payload: { context: 'account_deletion' },
    });
  });

  it('omits the verification transient payload when not provided', async () => {
    const calls: { body: unknown }[] = [];
    const transport: KratosTransport = {
      request(_method, _path, body) {
        calls.push({ body });
        return Promise.resolve({} as never);
      },
    };
    await submitVerificationEmail(
      transport,
      { id: 'f', ui: { action: '/self-service/verification?flow=f', method: 'POST' } },
      'a@b.c',
    );
    assert.deepEqual(calls[0]?.body, { method: 'code', email: 'a@b.c' });
  });

  it('logs out with DELETE', async () => {
    const calls: { method: string; body: unknown }[] = [];
    const transport: KratosTransport = {
      request(method, _path, body) {
        calls.push({ method, body });
        return Promise.resolve(undefined as never);
      },
    };
    await logout(transport, 'sess');
    assert.deepEqual(calls, [{ method: 'DELETE', body: { session_token: 'sess' } }]);
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
