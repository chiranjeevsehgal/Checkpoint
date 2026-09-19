import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  claudeCodeCommand,
  defaultKeyName,
  formatDateTime,
  mcpJsonSnippet,
  normalizeKeyName,
} from '../mcpKeysView.ts';

describe('defaultKeyName', () => {
  it('uses the model name when present', () => {
    assert.equal(defaultKeyName('Pixel 8', 'android'), 'Pixel 8');
    assert.equal(defaultKeyName('  iPhone XS  ', 'ios'), 'iPhone XS');
  });

  it('falls back to a platform label', () => {
    assert.equal(defaultKeyName(null, 'ios'), 'iOS');
    assert.equal(defaultKeyName(undefined, 'android'), 'Android');
  });
});

describe('normalizeKeyName', () => {
  it('trims and accepts a valid name', () => {
    assert.equal(normalizeKeyName('  laptop '), 'laptop');
  });

  it('rejects empty and over-long names', () => {
    assert.equal(normalizeKeyName('   '), null);
    assert.equal(normalizeKeyName('x'.repeat(101)), null);
  });

  it('accepts a name at the length limit', () => {
    assert.equal(normalizeKeyName('x'.repeat(100))?.length, 100);
  });
});

describe('connection snippets', () => {
  it('builds the Claude Code command', () => {
    const command = claudeCodeCommand('http://192.168.1.5:1417/mcp', 'cp_mcp_abc');
    assert.match(
      command,
      /claude mcp add --transport http checkpoint http:\/\/192\.168\.1\.5:1417\/mcp/,
    );
    assert.match(command, /Authorization: Bearer cp_mcp_abc/);
  });

  it('builds parseable mcpServers JSON', () => {
    const parsed = JSON.parse(mcpJsonSnippet('http://h:1417/mcp', 'cp_mcp_abc')) as {
      mcpServers: { checkpoint: { url: string; headers: { Authorization: string } } };
    };
    assert.equal(parsed.mcpServers.checkpoint.url, 'http://h:1417/mcp');
    assert.equal(parsed.mcpServers.checkpoint.headers.Authorization, 'Bearer cp_mcp_abc');
  });
});

describe('formatDateTime', () => {
  it('reads as Never when the timestamp is absent', () => {
    assert.equal(formatDateTime(undefined), 'Never');
  });

  it('renders a valid timestamp and tolerates junk', () => {
    assert.equal(formatDateTime('not-a-date'), '—');
    assert.notEqual(formatDateTime('2026-09-19T20:27:15Z'), '—');
  });
});
