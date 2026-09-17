import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { apiUrlForHost, kratosUrlForHost, normalizeHost } from '../server-urls.ts';

describe('normalizeHost', () => {
  it('accepts a bare LAN IP', () => {
    assert.equal(normalizeHost('192.168.1.5'), '192.168.1.5');
  });

  it('strips the scheme and port', () => {
    assert.equal(normalizeHost('http://192.168.1.5:8080'), '192.168.1.5');
    assert.equal(normalizeHost('https://192.168.1.5:4433/'), '192.168.1.5');
  });

  it('trims whitespace and ignores paths', () => {
    assert.equal(normalizeHost('  192.168.1.5/path '), '192.168.1.5');
  });

  it('returns null for input without a host', () => {
    assert.equal(normalizeHost(''), null);
    assert.equal(normalizeHost('   '), null);
    assert.equal(normalizeHost(null), null);
    assert.equal(normalizeHost('http://'), null);
  });
});

describe('server URL builders', () => {
  it('derives api and kratos URLs on fixed ports', () => {
    assert.equal(apiUrlForHost('192.168.1.5'), 'http://192.168.1.5:8080');
    assert.equal(kratosUrlForHost('192.168.1.5'), 'http://192.168.1.5:4433');
  });
});
