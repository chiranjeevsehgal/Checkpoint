import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

import { parseClaimHex } from '../claim.ts';
import { bytesToHex } from '../crypto.ts';

describe('parseClaimHex', () => {
  it('parses 64 hex chars', () => {
    assert.equal(bytesToHex(parseClaimHex('ab'.repeat(32))), 'ab'.repeat(32));
  });

  it('parses checkpoint://claim URI', () => {
    const key = parseClaimHex(
      'checkpoint://claim?key=' + 'cd'.repeat(32) + '&device=00112233445566778899aabbccddeeff',
    );
    assert.equal(bytesToHex(key), 'cd'.repeat(32));
  });

  it('rejects 16-byte device id with guidance', () => {
    assert.throws(() => parseClaimHex('ab'.repeat(16)), /device id/);
  });

  it('rejects garbage', () => {
    assert.throws(() => parseClaimHex('xyz'), /64 hex chars/);
  });

  it('rejects wrong length', () => {
    assert.throws(() => parseClaimHex('ab'.repeat(10)), /64 hex chars/);
  });
});
