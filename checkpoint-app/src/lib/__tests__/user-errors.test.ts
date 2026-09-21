import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { describeUserError } from '../user-errors.ts';

class StatusError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function abortError(): Error {
  const error = new Error('The operation was aborted.');
  error.name = 'AbortError';
  return error;
}

describe('describeUserError', () => {
  it('maps expired sessions to a sign-in prompt', () => {
    assert.equal(
      describeUserError(new StatusError(401, 'Request failed: 401'), 'Could not load.'),
      'Your session has expired. Please sign in again.',
    );
  });

  it('maps server failures to a retry prompt', () => {
    assert.equal(
      describeUserError(new StatusError(503, 'Request failed: 503'), 'Could not load.'),
      'Checkpoint is having trouble right now. Please try again in a moment.',
    );
  });

  it('maps connectivity failures to a connection prompt', () => {
    assert.equal(
      describeUserError(new TypeError('fetch failed'), 'Could not load.'),
      "Can't reach Checkpoint. Check your connection and try again.",
    );
    assert.equal(
      describeUserError(abortError(), 'Could not load.'),
      "Can't reach Checkpoint. Check your connection and try again.",
    );
  });

  it('never leaks raw messages for anything else', () => {
    assert.equal(
      describeUserError(new StatusError(400, 'Request failed: 400 {"error":"x"}'), 'Try again.'),
      'Try again.',
    );
    assert.equal(
      describeUserError(new Error('put failed (minio:9000): boom'), 'Upload failed.'),
      'Upload failed.',
    );
    assert.equal(describeUserError('boom', 'Upload failed.'), 'Upload failed.');
    assert.equal(describeUserError(null, 'Upload failed.'), 'Upload failed.');
  });
});
