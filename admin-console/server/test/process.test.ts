import { describe, expect, it } from 'vitest';

import { spawnError } from '../src/process';

describe('spawnError', () => {
  it('explains a missing executable', () => {
    const error = Object.assign(new Error('spawn arduino-cli ENOENT'), { code: 'ENOENT' });
    expect(spawnError(['arduino-cli', 'compile'], error).message).toBe(
      'arduino-cli was not found — set its path in Settings.',
    );
  });

  it('passes other errors through', () => {
    const error = new Error('boom');
    expect(spawnError(['x'], error)).toBe(error);
  });
});
