import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { buildDatabaseUrl, parseEnvFile, redactDatabaseUrl } from '../src/config';

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
