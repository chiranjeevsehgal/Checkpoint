import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { hexToBytes } from '../crypto.ts';
import { makeOggPage, oggCrc, parseOggPages, repairOpusOgg } from '../ogg.ts';

function hex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

describe('oggCrc', () => {
  it('matches client_app for OggS-test', () => {
    assert.equal(
      oggCrc(new TextEncoder().encode('OggS-test')).toString(16).padStart(8, '0'),
      '8093323b',
    );
  });
});

describe('repairOpusOgg', () => {
  const combinedIn = hexToBytes(
    '4f676753000200000000000000007856341200000000c9a20872' +
      '0213204f707573486561640101000000000000000000' +
      '4f70757354616773303132333435363738396162636465663031323334353637' +
      '4f6767530000c0030000000000007856341201000000ec9f928c' +
      '013c000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d' +
      '1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b',
  );
  const repairedHex =
    '4f676753000200000000000000007856341200000000b509cce4' +
    '01134f707573486561640101000000000000000000' +
    '4f6767530000000000000000000078563412010000008473452b' +
    '01204f70757354616773303132333435363738396162636465663031323334353637' +
    '4f6767530000c0030000000000007856341202000000165c1508' +
    '013c000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d' +
    '1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b';

  it('splits combined page 0 exactly like Python', () => {
    assert.equal(hex(repairOpusOgg(combinedIn)), repairedHex);
  });

  it('repaired output re-parses into 3 valid pages', () => {
    const pages = parseOggPages(repairOpusOgg(combinedIn));
    assert.equal(pages.length, 3);
  });

  it('leaves already-split streams untouched', () => {
    const head = new Uint8Array([
      ...new TextEncoder().encode('OpusHead'),
      1,
      1,
      0,
      0,
      0,
      0,
      0,
      0,
      0,
      0,
      0,
    ]);
    const tags = new Uint8Array([
      ...new TextEncoder().encode('OpusTags'),
      ...new TextEncoder().encode('0123456789abcdef01234567'),
    ]);
    const serial = 0x12345678;
    const split =
      hex(makeOggPage(0x02, BigInt(0), serial, 0, [19], head)) +
      hex(makeOggPage(0x00, BigInt(0), serial, 1, [32], tags));
    assert.equal(hex(repairOpusOgg(hexToBytes(split))), split);
  });

  it('throws on non-ogg input', () => {
    assert.throws(() => repairOpusOgg(new Uint8Array([1, 2, 3, 4])), /Invalid OGG page/);
  });
});
