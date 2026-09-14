import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  assertValidStoreKey,
  credentialKey,
  isValidStoreKey,
  sessionKey,
  settingsKey,
} from '../../../lib/storage/keys.ts';

describe('store key builders', () => {
  it('builds valid settings keys without slashes', () => {
    for (const name of ['serverUrl', 'vadThreshold']) {
      const key = settingsKey(name);
      assert.ok(!key.includes('/'), key);
      assert.ok(isValidStoreKey(key), key);
    }
  });

  it('builds valid credential keys without slashes', () => {
    const key = credentialKey('474f8bcaff162d3f18fbda36a168f201');
    assert.equal(key, 'Checkpoint.474f8bcaff162d3f18fbda36a168f201');
    assert.ok(isValidStoreKey(key));
  });

  it('builds a valid session key', () => {
    assert.equal(sessionKey(), 'checkpoint.session');
    assert.ok(isValidStoreKey(sessionKey()));
  });
});

describe('isValidStoreKey', () => {
  it('accepts alphanumerics plus . - _', () => {
    assert.ok(isValidStoreKey('Checkpoint.abc123'));
    assert.ok(isValidStoreKey('a-b_c.d'));
  });

  it('rejects empty keys and slashes', () => {
    assert.ok(!isValidStoreKey(''));
    assert.ok(!isValidStoreKey('checkpoint/settings/serverUrl'));
    assert.ok(!isValidStoreKey('Checkpoint/abcd'));
    assert.ok(!isValidStoreKey('has space'));
  });
});

describe('assertValidStoreKey', () => {
  it('throws a clear error for bad keys', () => {
    assert.throws(() => assertValidStoreKey('a/b'), /Invalid store key/);
  });

  it('passes good keys through', () => {
    assert.doesNotThrow(() => assertValidStoreKey('checkpoint.settings.x'));
  });
});
