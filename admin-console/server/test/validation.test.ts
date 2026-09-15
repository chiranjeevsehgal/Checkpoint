import { describe, expect, it } from 'vitest';

import { isClaimHash, isDeviceId, isUuid } from '../src/validation';
import { isDatabaseUrl, isHttpUrl, isSafeRemotePath } from '../src/validation';

const DEVICE = '474f8bcaff162d3f18fbda36a168f201';
const HASH = 'ab'.repeat(32);

describe('isDeviceId', () => {
  it('accepts 32 lowercase hex characters', () => {
    expect(isDeviceId(DEVICE)).toBe(true);
  });

  it('rejects uppercase, wrong length and non-hex', () => {
    expect(isDeviceId(DEVICE.toUpperCase())).toBe(false);
    expect(isDeviceId(DEVICE.slice(0, 31))).toBe(false);
    expect(isDeviceId('z'.repeat(32))).toBe(false);
  });
});

describe('isClaimHash', () => {
  it('accepts 64 lowercase hex characters', () => {
    expect(isClaimHash(HASH)).toBe(true);
  });

  it('rejects uppercase and wrong length', () => {
    expect(isClaimHash(HASH.toUpperCase())).toBe(false);
    expect(isClaimHash(HASH.slice(0, 63))).toBe(false);
  });
});

describe('isUuid', () => {
  it('accepts a canonical uuid', () => {
    expect(isUuid('4a49ce7d-de00-4dac-bb4c-85b1da408c7f')).toBe(true);
  });

  it('rejects junk', () => {
    expect(isUuid('not-a-uuid')).toBe(false);
  });
});

describe('isHttpUrl', () => {
  it('accepts http and https urls', () => {
    expect(isHttpUrl('http://127.0.0.1:4434')).toBe(true);
    expect(isHttpUrl('https://kratos.example.com')).toBe(true);
  });

  it('rejects other protocols and junk', () => {
    expect(isHttpUrl('postgres://db')).toBe(false);
    expect(isHttpUrl('not a url')).toBe(false);
  });
});

describe('isDatabaseUrl', () => {
  it('accepts postgres urls', () => {
    expect(isDatabaseUrl('postgres://user:pw@localhost:5432/db?sslmode=disable')).toBe(true);
    expect(isDatabaseUrl('postgresql://user@host/db')).toBe(true);
  });

  it('rejects other urls', () => {
    expect(isDatabaseUrl('http://localhost')).toBe(false);
  });
});

describe('isSafeRemotePath', () => {
  it('accepts simple remote paths', () => {
    expect(isSafeRemotePath('/srv/checkpoint/ingestion-service')).toBe(true);
    expect(isSafeRemotePath('~/checkpoint')).toBe(true);
  });

  it('rejects shell metacharacters and spaces', () => {
    expect(isSafeRemotePath('/srv/my dir')).toBe(false);
    expect(isSafeRemotePath('/srv;rm -rf')).toBe(false);
  });
});
