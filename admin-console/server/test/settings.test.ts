import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { pickSettings, readSettings, settingsPath, writeSettings } from '../src/settings';

function tempRepo(): string {
  return mkdtempSync(join(tmpdir(), 'ck-admin-'));
}

describe('settings store', () => {
  it('returns empty when no settings file exists', () => {
    expect(readSettings(tempRepo())).toEqual({});
  });

  it('round-trips known keys', () => {
    const repo = tempRepo();
    writeSettings(repo, { ADMIN_ARDUINO_CLI: 'C:/arduino-cli.exe' });
    const raw = JSON.parse(readFileSync(settingsPath(repo), 'utf8')) as object;
    expect(raw).toEqual({ ADMIN_ARDUINO_CLI: 'C:/arduino-cli.exe' });
    expect(readSettings(repo)).toEqual({ ADMIN_ARDUINO_CLI: 'C:/arduino-cli.exe' });
  });

  it('ignores unknown keys and blank values', () => {
    expect(
      pickSettings({ ADMIN_NOPE: 'x', ADMIN_FQBN: '  ', ADMIN_INGESTION_URL: 'http://x' }),
    ).toEqual({ ADMIN_INGESTION_URL: 'http://x' });
  });
});
