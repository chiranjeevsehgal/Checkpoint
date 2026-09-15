import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { buildDatabaseUrl, parseEnvFile, redactDatabaseUrl, resolveEnv } from '../src/config';
import { writeSettings } from '../src/settings';

describe('buildDatabaseUrl', () => {
  it('builds a host DSN from POSTGRES_* values', () => {
    expect(
      buildDatabaseUrl({ POSTGRES_USER: 'checkpoint', POSTGRES_PASSWORD: 'secret', POSTGRES_DB: 'checkpoint_db' }),
    ).toBe('postgres://checkpoint:secret@localhost:5432/checkpoint_db?sslmode=disable');
  });

  it('returns empty when required values are missing', () => {
    expect(buildDatabaseUrl({ POSTGRES_DB: 'checkpoint_db' })).toBe('');
    expect(buildDatabaseUrl({})).toBe('');
  });
});

describe('parseEnvFile', () => {
  it('reads key=value pairs and ignores comments and blanks', () => {
    const file = join(mkdtempSync(join(tmpdir(), 'ck-env-')), '.env');
    writeFileSync(file, '# comment\n\nPOSTGRES_USER=checkpoint\nPOSTGRES_DB=checkpoint_db\n');
    expect(parseEnvFile(file)).toEqual({ POSTGRES_USER: 'checkpoint', POSTGRES_DB: 'checkpoint_db' });
  });

  it('returns empty for a missing file', () => {
    expect(parseEnvFile(join(tmpdir(), 'ck-missing.env'))).toEqual({});
  });
});

describe('redactDatabaseUrl', () => {
  it('hides the password', () => {
    expect(redactDatabaseUrl('postgres://user:secret@localhost:5432/db')).toBe(
      'postgres://user:****@localhost:5432/db',
    );
  });
});

describe('resolveEnv precedence', () => {
  function fakeRepo(): string {
    const root = mkdtempSync(join(tmpdir(), 'ck-repo-'));
    mkdirSync(join(root, 'ingestion-service', 'cmd', 'device-admin'), { recursive: true });
    mkdirSync(join(root, 'admin-console'), { recursive: true });
    writeFileSync(join(root, '.env'), 'ADMIN_INGESTION_URL=http://repo\nADMIN_ARDUINO_CLI=repo-cli\n');
    writeFileSync(join(root, 'admin-console', '.env'), 'ADMIN_ARDUINO_CLI=console-cli\n');
    writeSettings(root, { ADMIN_ARDUINO_CLI: 'settings-cli', ADMIN_SKETCH_DIR: 'custom/sketch' });
    return root;
  }

  it('prefers settings.json over .env files', () => {
    const { env } = resolveEnv(fakeRepo());
    expect(env.ADMIN_ARDUINO_CLI).toBe('settings-cli');
    expect(env.ADMIN_SKETCH_DIR).toBe('custom/sketch');
    expect(env.ADMIN_INGESTION_URL).toBe('http://repo');
  });

  it('prefers the real environment over everything', () => {
    const previous = process.env.ADMIN_ARDUINO_CLI;
    process.env.ADMIN_ARDUINO_CLI = 'from-process';
    try {
      expect(resolveEnv(fakeRepo()).env.ADMIN_ARDUINO_CLI).toBe('from-process');
    } finally {
      if (previous === undefined) delete process.env.ADMIN_ARDUINO_CLI;
      else process.env.ADMIN_ARDUINO_CLI = previous;
    }
  });

  it('reports the winning source', () => {
    const { sources } = resolveEnv(fakeRepo());
    expect(sources.ADMIN_ARDUINO_CLI).toBe('settings');
    expect(sources.ADMIN_INGESTION_URL).toBe('repo .env');
  });
});
