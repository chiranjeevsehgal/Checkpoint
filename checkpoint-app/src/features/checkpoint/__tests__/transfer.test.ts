import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  IncomingFile,
  contigOf,
  fileIdHex,
  resumeFrom,
  shouldSendAck,
  validateSidecar,
} from '../transfer.ts';

describe('fileIdHex', () => {
  it('pads to 16 hex chars', () => {
    assert.equal(fileIdHex(BigInt('0x1122334455667788')), '1122334455667788');
    assert.equal(fileIdHex(BigInt(1)), '0000000000000001');
  });
});

describe('IncomingFile', () => {
  it('reassembles out-of-order fragments', () => {
    const file = new IncomingFile(BigInt(1), 9, 3, 0, null, 1, 3);
    assert.equal(file.addFragment(2, new Uint8Array([7, 8, 9])), true);
    assert.equal(file.contigSeq, -1);
    assert.equal(file.addFragment(0, new Uint8Array([1, 2, 3])), true);
    assert.equal(file.contigSeq, 0);
    assert.equal(file.addFragment(1, new Uint8Array([4, 5, 6])), true);
    assert.equal(file.contigSeq, 2);
    assert.equal(file.isComplete(), true);
    assert.deepEqual(Array.from(file.data()), [1, 2, 3, 4, 5, 6, 7, 8, 9]);
  });

  it('rejects out-of-range fragments', () => {
    const file = new IncomingFile(BigInt(1), 6, 2, 0, null, 1, 3);
    assert.equal(file.addFragment(5, new Uint8Array([1, 2, 3])), false);
    assert.equal(file.isComplete(), false);
  });
});

describe('contigOf', () => {
  it('finds the contiguous run', () => {
    assert.equal(contigOf(new Set()), -1);
    assert.equal(contigOf(new Set([0, 1, 2])), 2);
    assert.equal(contigOf(new Set([1, 2])), -1);
    assert.equal(contigOf(new Set([0, 2, 3])), 0);
  });
});

describe('validateSidecar', () => {
  const good = {
    crc: 'deadbeef',
    total: 660,
    totalFrags: 3,
    fragSize: 220,
    received: [0, 1],
  };

  it('accepts matching state', () => {
    assert.deepEqual(validateSidecar(good, 'deadbeef', 660, 3, 220, 660), [0, 1]);
  });

  it('rejects crc mismatch', () => {
    assert.equal(validateSidecar(good, '00000000', 660, 3, 220, 660), null);
  });

  it('rejects total mismatch', () => {
    assert.equal(validateSidecar(good, 'deadbeef', 661, 3, 220, 660), null);
  });

  it('drops seqs beyond bytes on disk', () => {
    assert.deepEqual(validateSidecar(good, 'deadbeef', 660, 3, 220, 220), [0]);
  });

  it('rejects null', () => {
    assert.equal(validateSidecar(null, 'deadbeef', 660, 3, 220, 660), null);
  });
});

describe('resumeFrom', () => {
  it('continues after the contiguous run', () => {
    assert.equal(resumeFrom([0, 1, 3], 5), 2);
  });

  it('returns 0 with no progress or when complete', () => {
    assert.equal(resumeFrom([], 5), 0);
    assert.equal(resumeFrom([0, 1, 2, 3, 4], 5), 0);
  });
});

describe('shouldSendAck', () => {
  it('acks every 8-frag window', () => {
    assert.equal(shouldSendAck(8, 7, 100), true);
    assert.equal(shouldSendAck(16, 15, 100), true);
    assert.equal(shouldSendAck(9, 8, 100), false);
    assert.equal(shouldSendAck(7, 6, 100), false);
    assert.equal(shouldSendAck(100, 99, 100), true);
    assert.equal(shouldSendAck(3, -1, 100), false);
  });
});
