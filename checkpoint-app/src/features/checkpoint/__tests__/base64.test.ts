import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { base64Decode, base64Encode } from '../base64.ts';

function hex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

describe('base64', () => {
  it('encodes without padding', () => {
    assert.equal(base64Encode(new Uint8Array([0x00, 0xff, 0x10])), 'AP8Q');
  });

  it('encodes with padding', () => {
    assert.equal(base64Encode(new TextEncoder().encode('hello')), 'aGVsbG8=');
    assert.equal(base64Encode(new Uint8Array([0xff])), '/w==');
    assert.equal(base64Encode(new Uint8Array([0xff, 0xff])), '//8=');
  });

  it('round-trips binary data', () => {
    const data = new Uint8Array(256);
    for (let i = 0; i < 256; i++) data[i] = i;
    assert.equal(hex(base64Decode(base64Encode(data))), hex(data));
  });

  it('rejects bad input', () => {
    assert.throws(() => base64Decode('abc'), /length/);
    assert.throws(() => base64Decode('****'), /char/);
  });
});
