import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  CLIENT_DOMAIN,
  SERVER_DOMAIN,
  buildNonce,
  buildTranscript,
  bytesToHex,
  clientProof,
  constantTimeEqual,
  decryptFragment,
  deriveClientKeyV3,
  deriveFileKey,
  deriveSessionKeyV3,
  finishProof,
  hexToBytes,
  hkdfSha256,
  newId,
} from '../crypto.ts';

const FILE_ID = BigInt('0x1122334455667788');
const SESSION_ID = 2864434397;
const DEVICE_ID = hexToBytes('000102030405060708090a0b0c0d0e0f');
const CLIENT_ID = hexToBytes('101112131415161718191a1b1c1d1e1f');
const DEVICE_NONCE = hexToBytes('202122232425262728292a2b2c2d2e2f');
const CLIENT_NONCE = hexToBytes('303132333435363738393a3b3c3d3e3f');
const CLIENT_KEY = hexToBytes('404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f');
const CLAIM_KEY = hexToBytes('606162636465666768696a6b6c6d6e6f707172737475767778797a7b7c7d7e7f');

describe('hkdfSha256', () => {
  it('matches client_app single-block output', () => {
    const salt = hexToBytes('000102030405060708090a0b0c0d0e0f');
    const ikm = hexToBytes('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f');
    const out = hkdfSha256(salt, ikm, new TextEncoder().encode('test-info'), 16);
    assert.equal(bytesToHex(out), '76d3c55008225ab5f96d600a215b1711');
  });
});

describe('key derivation', () => {
  it('deriveFileKey matches', () => {
    assert.equal(
      bytesToHex(deriveFileKey(new Uint8Array(16), 0x12345678, FILE_ID)),
      'd44a89371cfe084f756cd48037798657',
    );
  });

  it('transcript matches', () => {
    const transcript = buildTranscript(
      DEVICE_ID,
      CLIENT_ID,
      SESSION_ID,
      DEVICE_NONCE,
      CLIENT_NONCE,
      0,
    );
    assert.equal(
      bytesToHex(transcript),
      '636865636b706f696e742d617574682d7633000102030405060708090a0b0c0d0e0f' +
        '101112131415161718191a1b1c1d1e1f' +
        'ddccbbaa' +
        '202122232425262728292a2b2c2d2e2f' +
        '303132333435363738393a3b3c3d3e3f' +
        '00',
    );
  });

  it('clientProof matches', () => {
    const transcript = buildTranscript(
      DEVICE_ID,
      CLIENT_ID,
      SESSION_ID,
      DEVICE_NONCE,
      CLIENT_NONCE,
      0,
    );
    assert.equal(
      bytesToHex(clientProof(CLIENT_KEY, transcript)),
      '9056dd22a2d88daec4d26b24d097d94595e62b2ffc7a7de4a4e587e42fadba86',
    );
  });

  it('deriveSessionKeyV3 matches', () => {
    assert.equal(
      bytesToHex(
        deriveSessionKeyV3(
          CLIENT_KEY,
          DEVICE_NONCE,
          CLIENT_NONCE,
          DEVICE_ID,
          CLIENT_ID,
          SESSION_ID,
        ),
      ),
      '7c1113a568d5b1f824ed62a5caa42732',
    );
  });

  it('finish proofs match', () => {
    const sessionKey = hexToBytes('7c1113a568d5b1f824ed62a5caa42732');
    const transcript = buildTranscript(
      DEVICE_ID,
      CLIENT_ID,
      SESSION_ID,
      DEVICE_NONCE,
      CLIENT_NONCE,
      0,
    );
    assert.equal(
      bytesToHex(finishProof(sessionKey, SERVER_DOMAIN, transcript)),
      'cfb72b0437a3bb4c245a6e30c9e10e8d29d2459a98970962ce4319f08a2d78c1',
    );
    assert.equal(
      bytesToHex(finishProof(sessionKey, CLIENT_DOMAIN, transcript)),
      'aefb614aec5e2bcdd072e70d0ea9200003bcc2ccd5dc19c553e99cdef3e27314',
    );
  });

  it('deriveClientKeyV3 matches', () => {
    assert.equal(
      bytesToHex(deriveClientKeyV3(CLAIM_KEY, DEVICE_NONCE, CLIENT_NONCE, DEVICE_ID, CLIENT_ID)),
      '0b7dabd8f60fee60f894c8a52cacfbc24e8fb874bd9b0e3e3646cb2081fdcddb',
    );
  });

  it('buildNonce matches', () => {
    assert.equal(bytesToHex(buildNonce(SESSION_ID, FILE_ID, 7)), '219ab010240a4f3b852b8e4e');
  });
});

describe('decryptFragment', () => {
  const key = hexToBytes('7c1113a568d5b1f824ed62a5caa42732');

  it('decrypts the 220-byte Python-produced fragment', () => {
    const cipher = hexToBytes(
      'c703e6c8824d4d3251c60012253a978c46d94688845e4d28e901640b625a2d67c5' +
        '018c13a1c274561d77ea4323afc69c44e0664f659d5f19083374030d59424e10' +
        'e29aa572ffd3cd15e74594f50940ccec0bf215a3adf33eac546e9143c1f1588' +
        '2fd6daf30d2007e5c45f86ba47954b0162b82a0bb0c64f4cad0cab70e5b8bf9' +
        '837434d55ad7364cfc0512eb065184ebbfa2e6b922466248e3ad44e3c9a977' +
        '8440920ba69bef53d76a496f5d6b8bb55d86b9b954edc6dcb07f09f310d77d' +
        'bb2c376b61c883ce949ece42ecbd5fabceb73e45248895e1d96f08a0c0fbf0' +
        '7605da4e608462',
    );
    const plain = decryptFragment(key, SESSION_ID, FILE_ID, 7, cipher);
    if (!plain) throw new Error('decrypt returned null');
    assert.equal(
      bytesToHex(plain),
      '030a11181f262d343b424950575e656c737a81888f969da4abb2b9c0c7ced5dc' +
        'e3eaf1f8ff060d141b222930373e454c535a61686f767d848b9299a0a7aeb5' +
        'bcc3cad1d8dfe6edf4fb020910171e252c333a41484f565d646b727980878e' +
        '959ca3aab1b8bfc6cdd4dbe2e9f0f7fe050c131a21282f363d444b52596067' +
        '6e757c838a91989fa6adb4bbc2c9d0d7dee5ecf3fa01080f161d242b323940' +
        '474e555c636a71787f868d949ba2a9b0b7bec5ccd3dae1e8eff6fd040b1219' +
        '20272e353c434a51585f666d747b828990979ea5acb3bac1c8cfd6dde4ebf2f900',
    );
  });

  it('decrypts the short fragment', () => {
    const plain = decryptFragment(
      key,
      SESSION_ID,
      FILE_ID,
      0,
      hexToBytes('1b10af0ccf4d4b42d227177b96b7feec679d437f74910d9cc5511e4bff'),
    );
    if (!plain) throw new Error('decrypt returned null');
    assert.equal(bytesToHex(plain), '68656c6c6f2d6f7075732d62797465732d31323334');
  });

  it('rejects tampered tag', () => {
    const cipher = hexToBytes('1b10af0ccf4d4b42d227177b96b7feec679d437f74910d9cc5511e4b00');
    assert.equal(decryptFragment(key, SESSION_ID, FILE_ID, 0, cipher), null);
  });

  it('rejects wrong seq', () => {
    const cipher = hexToBytes('1b10af0ccf4d4b42d227177b96b7feec679d437f74910d9cc5511e4bff');
    assert.equal(decryptFragment(key, SESSION_ID, FILE_ID, 9, cipher), null);
  });

  it('rejects input shorter than tag', () => {
    assert.equal(decryptFragment(key, SESSION_ID, FILE_ID, 0, new Uint8Array(4)), null);
  });
});

describe('helpers', () => {
  it('constantTimeEqual compares', () => {
    assert.equal(constantTimeEqual(new Uint8Array([1, 2]), new Uint8Array([1, 2])), true);
    assert.equal(constantTimeEqual(new Uint8Array([1, 2]), new Uint8Array([1, 3])), false);
    assert.equal(constantTimeEqual(new Uint8Array([1]), new Uint8Array([1, 2])), false);
  });

  it('hexToBytes rejects odd length', () => {
    assert.throws(() => hexToBytes('abc'), /Odd-length/);
  });

  it('newId returns 16 bytes', () => {
    assert.equal(newId().length, 16);
  });
});
