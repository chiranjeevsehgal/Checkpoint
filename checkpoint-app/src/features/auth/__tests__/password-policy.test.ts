import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { PASSWORD_MIN_LENGTH, passwordMeetsLength } from '../password-policy.ts';

describe('passwordMeetsLength', () => {
  it('rejects a password below the minimum length', () => {
    assert.equal(passwordMeetsLength('a'.repeat(PASSWORD_MIN_LENGTH - 1)), false);
  });

  it('accepts a password at the minimum length', () => {
    assert.equal(passwordMeetsLength('a'.repeat(PASSWORD_MIN_LENGTH)), true);
  });

  it('accepts a longer password', () => {
    assert.equal(passwordMeetsLength('a'.repeat(PASSWORD_MIN_LENGTH + 5)), true);
  });
});
