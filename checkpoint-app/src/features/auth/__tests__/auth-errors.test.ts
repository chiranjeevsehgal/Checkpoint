import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { describeAuthError } from '../auth-errors.ts';
import { KratosError } from '../kratos-client.ts';

class FakeApiError extends Error {
  status: number;
  code?: string;

  constructor(status: number, code?: string) {
    super('fake');
    this.status = status;
    this.code = code;
  }
}

describe('describeAuthError', () => {
  it('maps password policy messages from Kratos', () => {
    const error = new KratosError(400, undefined, [
      'the password does not fulfill the password policy because: The password must be at least 12 characters long, but got 10.',
    ]);
    assert.equal(describeAuthError(error, 'fallback'), 'Use at least 12 characters.');
  });

  it('maps a breached password', () => {
    const error = new KratosError(400, undefined, [
      'The password has been found in data breaches and must no longer be used.',
    ]);
    assert.equal(
      describeAuthError(error, 'fallback'),
      'That password has appeared in a data breach. Choose a different one.',
    );
  });

  it('maps a password similar to the identifier', () => {
    const error = new KratosError(400, undefined, [
      'The password is too similar to the identifier.',
    ]);
    assert.equal(
      describeAuthError(error, 'fallback'),
      'Choose a password that is not similar to your email or name.',
    );
  });

  it('maps reusing the current password', () => {
    const error = new KratosError(400, undefined, [
      'The new password must be different from the old password.',
    ]);
    assert.equal(
      describeAuthError(error, 'fallback'),
      'Your new password must be different from your current password.',
    );
  });

  it('maps an invalid verification code', () => {
    const error = new KratosError(400, undefined, [
      'The verification code is invalid or has already been used.',
    ]);
    assert.equal(
      describeAuthError(error, 'fallback'),
      'That code is incorrect or has expired. Request a new one.',
    );
  });

  it('maps invalid sign-in credentials', () => {
    const error = new KratosError(400, undefined, [
      'The provided credentials are invalid, check for spelling mistakes in your password or username, email address.',
    ]);
    assert.equal(describeAuthError(error, 'fallback'), 'Incorrect email or password.');
  });

  it('maps an existing account', () => {
    const error = new KratosError(400, undefined, [
      'An account with the same identifier already exists.',
    ]);
    assert.equal(
      describeAuthError(error, 'fallback'),
      'An account with this email already exists. Try signing in instead.',
    );
  });

  it('falls back to status-based messages for Kratos errors', () => {
    assert.equal(
      describeAuthError(new KratosError(401, undefined, []), 'fallback'),
      'Your session has expired. Please sign in again.',
    );
    assert.equal(
      describeAuthError(new KratosError(403, undefined, []), 'fallback'),
      'Please sign in again to continue.',
    );
    assert.equal(
      describeAuthError(new KratosError(410, undefined, []), 'fallback'),
      'That request has expired. Please start again.',
    );
    assert.equal(
      describeAuthError(new KratosError(503, undefined, []), 'fallback'),
      'Checkpoint is having trouble right now. Please try again in a moment.',
    );
  });

  it('uses the caller fallback for an unmapped Kratos 400', () => {
    assert.equal(
      describeAuthError(new KratosError(400, undefined, []), 'my fallback'),
      'my fallback',
    );
  });

  it('maps API error codes', () => {
    assert.equal(
      describeAuthError(new FakeApiError(403, 'REAUTH_REQUIRED'), 'fallback'),
      'That confirmation expired. Send a new code and try again.',
    );
    assert.equal(
      describeAuthError(new FakeApiError(403, 'ACCOUNT_DELETING'), 'fallback'),
      'This account is already being deleted.',
    );
    assert.equal(
      describeAuthError(new FakeApiError(401), 'fallback'),
      'Your session has expired. Please sign in again.',
    );
    assert.equal(
      describeAuthError(new FakeApiError(500), 'fallback'),
      'Checkpoint is having trouble right now. Please try again in a moment.',
    );
  });

  it('maps a network failure', () => {
    assert.equal(
      describeAuthError(new TypeError('Network request failed'), 'fallback'),
      "Can't reach Checkpoint. Check your connection and try again.",
    );
  });

  it('uses the fallback for unknown errors', () => {
    assert.equal(describeAuthError(new Error('boom'), 'my fallback'), 'my fallback');
    assert.equal(describeAuthError('nope', 'my fallback'), 'my fallback');
  });
});
