import { ecb } from '@noble/ciphers/aes.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';

import {
  AUTH_CLOUD_SECRET_BYTES,
  CRYPTO_KEY_BYTES,
  CRYPTO_NONCE_BYTES,
  CRYPTO_TAG_BYTES,
  CTRL_CMD_GET_CLOUD_SECRET,
  PKT_DATA,
  PROTO_VER,
} from './config.ts';

export const AUTH_DOMAIN = 'checkpoint-auth-v3';
export const SERVER_DOMAIN = 'checkpoint-server-finish-v3';
export const CLIENT_DOMAIN = 'checkpoint-client-finish-v3';
const SESSION_INFO = 'checkpoint-session-v3';
const ENROLL_INFO = 'checkpoint-client-v3';
const FILE_INFO = 'checkpoint-file-v1';
const NONCE_INFO = 'checkpoint-nonce-v1';
const CLOUD_NONCE_INFO = 'checkpoint-cloud-v1';

const textEncoder = new TextEncoder();

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

export function hexToBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0) throw new Error('Odd-length hex string');
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    const byte = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
    if (Number.isNaN(byte)) throw new Error('Invalid hex string');
    out[i] = byte;
  }
  return out;
}

export function newId(): Uint8Array {
  const webCrypto = (globalThis as { crypto?: Crypto }).crypto;
  if (typeof webCrypto?.getRandomValues === 'function') {
    return webCrypto.getRandomValues(new Uint8Array(16));
  }
  // Hermes builds without WebCrypto fall back to the native module. This stays
  // a lazy require (not a static import) so node unit tests never load native code.
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const expoCrypto = require('expo-crypto') as {
    getRandomBytes(byteCount: number): Uint8Array;
  };
  return expoCrypto.getRandomBytes(16);
}

export function constantTimeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i]! ^ b[i]!;
  return diff === 0;
}

export function hkdfSha256(
  salt: Uint8Array,
  ikm: Uint8Array,
  info: Uint8Array,
  length: number,
): Uint8Array {
  const prk = hmac(sha256, salt, ikm);
  const okm = hmac(sha256, prk, concat(info, new Uint8Array([0x01])));
  return okm.slice(0, length);
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

function packSession(sessionId: number): Uint8Array {
  const out = new Uint8Array(4);
  new DataView(out.buffer).setUint32(0, sessionId >>> 0, true);
  return out;
}

function packFileId(fileId: bigint): Uint8Array {
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, fileId, true);
  return out;
}

export function deriveFileKey(
  sessionKey: Uint8Array,
  sessionId: number,
  fileId: bigint,
): Uint8Array {
  const info = concat(textEncoder.encode(FILE_INFO), packSession(sessionId), packFileId(fileId));
  return hkdfSha256(new Uint8Array(0), sessionKey, info, CRYPTO_KEY_BYTES);
}

export function buildTranscript(
  deviceId: Uint8Array,
  clientId: Uint8Array,
  sessionId: number,
  deviceNonce: Uint8Array,
  clientNonce: Uint8Array,
  mode: number,
): Uint8Array {
  return concat(
    textEncoder.encode(AUTH_DOMAIN),
    deviceId,
    clientId,
    packSession(sessionId),
    deviceNonce,
    clientNonce,
    new Uint8Array([mode & 0xff]),
  );
}

export function deriveSessionKeyV3(
  clientKey: Uint8Array,
  deviceNonce: Uint8Array,
  clientNonce: Uint8Array,
  deviceId: Uint8Array,
  clientId: Uint8Array,
  sessionId: number,
): Uint8Array {
  const salt = concat(deviceNonce, clientNonce);
  const info = concat(textEncoder.encode(SESSION_INFO), deviceId, clientId, packSession(sessionId));
  return hkdfSha256(salt, clientKey, info, CRYPTO_KEY_BYTES);
}

export function deriveClientKeyV3(
  claimKey: Uint8Array,
  deviceNonce: Uint8Array,
  clientNonce: Uint8Array,
  deviceId: Uint8Array,
  clientId: Uint8Array,
): Uint8Array {
  const salt = concat(deviceNonce, clientNonce);
  const info = concat(textEncoder.encode(ENROLL_INFO), deviceId, clientId);
  return hkdfSha256(salt, claimKey, info, 32);
}

export function clientProof(clientKey: Uint8Array, transcript: Uint8Array): Uint8Array {
  return hmac(sha256, clientKey, transcript);
}

export function finishProof(
  sessionKey: Uint8Array,
  domain: string,
  transcript: Uint8Array,
): Uint8Array {
  return hmac(sha256, sessionKey, concat(textEncoder.encode(domain), transcript));
}

export function buildNonce(sessionId: number, fileId: bigint, seq: number): Uint8Array {
  const seqBytes = new Uint8Array(2);
  new DataView(seqBytes.buffer).setUint16(0, seq & 0xffff, true);
  const msg = concat(
    textEncoder.encode(NONCE_INFO),
    packSession(sessionId),
    packFileId(fileId),
    seqBytes,
  );
  return sha256(msg).slice(0, CRYPTO_NONCE_BYTES);
}

type AesBlock = ReturnType<typeof ecb>;

/** Expands an AES-128 key using the vetted @noble/ciphers primitive. */
function expandKey(key: Uint8Array): AesBlock {
  if (key.length !== 16) throw new Error('AES-128 key must be 16 bytes');
  // CCM frames its own 16-byte blocks, so the padding layer stays off.
  return ecb(key, { disablePadding: true });
}

export function aesEncryptBlock(key: Uint8Array, block: Uint8Array): Uint8Array {
  if (block.length !== 16) throw new Error('AES block must be 16 bytes');
  return expandKey(key).encrypt(block);
}

function ccmCounterBlock(nonce: Uint8Array, counter: number): Uint8Array {
  const block = new Uint8Array(16);
  block[0] = 2;
  block.set(nonce, 1);
  block[13] = (counter >>> 16) & 0xff;
  block[14] = (counter >>> 8) & 0xff;
  block[15] = counter & 0xff;
  return block;
}

function ccmMac(
  key: Uint8Array,
  nonce: Uint8Array,
  aad: Uint8Array,
  message: Uint8Array,
): Uint8Array {
  const firstFlags = (aad.length > 0 ? 64 : 0) | 24 | 2;
  const b0 = new Uint8Array(16);
  b0[0] = firstFlags;
  b0.set(nonce, 1);
  const q = new DataView(b0.buffer, 13, 3);
  q.setUint8(0, (message.length >>> 16) & 0xff);
  q.setUint8(1, (message.length >>> 8) & 0xff);
  q.setUint8(2, message.length & 0xff);
  const blocks: Uint8Array[] = [b0];
  if (aad.length > 0) {
    const aadLen = new Uint8Array(2);
    new DataView(aadLen.buffer).setUint16(0, aad.length, false);
    const formatted = concat(aadLen, aad);
    for (let offset = 0; offset < formatted.length; offset += 16) {
      const chunk = new Uint8Array(16);
      chunk.set(formatted.slice(offset, offset + 16), 0);
      blocks.push(chunk);
    }
  }
  for (let offset = 0; offset < message.length; offset += 16) {
    const chunk = new Uint8Array(16);
    chunk.set(message.slice(offset, offset + 16), 0);
    blocks.push(chunk);
  }
  let mac: Uint8Array = new Uint8Array(16);
  for (const block of blocks) {
    const xored = new Uint8Array(16);
    for (let i = 0; i < 16; i++) xored[i] = mac[i]! ^ block[i]!;
    mac = aesEncryptBlock(key, xored);
  }
  return mac;
}

function ccmCrypt(key: Uint8Array, nonce: Uint8Array, message: Uint8Array): Uint8Array {
  const out = new Uint8Array(message.length);
  let counter = 1;
  for (let offset = 0; offset < message.length; offset += 16) {
    const keystream = aesEncryptBlock(key, ccmCounterBlock(nonce, counter));
    counter += 1;
    const end = Math.min(offset + 16, message.length);
    for (let i = offset; i < end; i++) out[i] = message[i]! ^ keystream[i - offset]!;
  }
  return out;
}

export function buildCloudNonce(sessionId: number, seq: number): Uint8Array {
  const seqBytes = new Uint8Array(2);
  new DataView(seqBytes.buffer).setUint16(0, seq & 0xffff, true);
  return sha256(
    concat(textEncoder.encode(CLOUD_NONCE_INFO), packSession(sessionId), seqBytes),
  ).slice(0, CRYPTO_NONCE_BYTES);
}

/** Opens the GET_CLOUD_SECRET response: nonce(12) || cipher(32) || tag(8). */
export function openCloudSecret(
  sessionKey: Uint8Array,
  sessionId: number,
  seq: number,
  envelope: Uint8Array,
): Uint8Array | null {
  const expectedLen = CRYPTO_NONCE_BYTES + AUTH_CLOUD_SECRET_BYTES + CRYPTO_TAG_BYTES;
  if (envelope.length !== expectedLen) return null;
  const nonce = envelope.slice(0, CRYPTO_NONCE_BYTES);
  if (!constantTimeEqual(nonce, buildCloudNonce(sessionId, seq))) return null;
  const body = envelope.slice(CRYPTO_NONCE_BYTES);
  const cipherBytes = body.slice(0, AUTH_CLOUD_SECRET_BYTES);
  const receivedTag = body.slice(AUTH_CLOUD_SECRET_BYTES);
  const aad = new Uint8Array(4);
  aad[0] = PROTO_VER;
  aad[1] = CTRL_CMD_GET_CLOUD_SECRET;
  aad[2] = seq & 0xff;
  aad[3] = (seq >> 8) & 0xff;
  const plain = ccmCrypt(sessionKey, nonce, cipherBytes);
  const mac = ccmMac(sessionKey, nonce, aad, plain);
  const mask = aesEncryptBlock(sessionKey, ccmCounterBlock(nonce, 0));
  const expectedTag = new Uint8Array(CRYPTO_TAG_BYTES);
  for (let i = 0; i < CRYPTO_TAG_BYTES; i++) {
    expectedTag[i] = mac[i]! ^ mask[i]!;
  }
  return constantTimeEqual(expectedTag, receivedTag) ? plain : null;
}

export function decryptFragment(
  key: Uint8Array,
  sessionId: number,
  fileId: bigint,
  seq: number,
  cipherAndTag: Uint8Array,
): Uint8Array | null {
  if (cipherAndTag.length < CRYPTO_TAG_BYTES) return null;
  const fragLen = cipherAndTag.length - CRYPTO_TAG_BYTES;
  const nonce = buildNonce(sessionId, fileId, seq);
  const aad = new Uint8Array(6);
  const aadView = new DataView(aad.buffer);
  aadView.setUint8(0, PROTO_VER);
  aadView.setUint8(1, PKT_DATA);
  aadView.setUint16(2, seq & 0xffff, true);
  aadView.setUint16(4, fragLen, true);
  const cipherBytes = cipherAndTag.slice(0, fragLen);
  const receivedTag = cipherAndTag.slice(fragLen);
  const plain = ccmCrypt(key, nonce, cipherBytes);
  const mac = ccmMac(key, nonce, aad, plain);
  const mask = aesEncryptBlock(key, ccmCounterBlock(nonce, 0));
  const expectedTag = new Uint8Array(CRYPTO_TAG_BYTES);
  for (let i = 0; i < CRYPTO_TAG_BYTES; i++) {
    expectedTag[i] = mac[i]! ^ mask[i]!;
  }
  if (!constantTimeEqual(expectedTag, receivedTag)) return null;
  return plain;
}
