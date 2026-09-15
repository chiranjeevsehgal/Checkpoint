import { describe, expect, it } from 'vitest';

import type { AppConfig } from '../src/config';
import { buildDeviceAdminArgs, deviceAdminInvocation, parseProvision, shellQuote } from '../src/deviceAdmin';

const DEVICE = '474f8bcaff162d3f18fbda36a168f201';
const HASH = 'ab'.repeat(32);

const baseConfig: AppConfig = {
  host: '127.0.0.1',
  port: 4300,
  repoRoot: '/repo',
  databaseUrl: 'postgres://user:secret@localhost:5432/db',
  kratosAdminUrl: 'http://127.0.0.1:4434',
  kratosPublicUrl: 'http://127.0.0.1:4433',
  ingestionUrl: 'http://127.0.0.1:8080',
  arduinoCli: 'arduino-cli',
  sketchDir: '/repo/firmware/checkpoint',
  buildDir: '/repo/firmware/checkpoint/build',
  fqbn: 'esp32:esp32:esp32s3',
  dockerHost: '',
  deviceAdminMode: 'go',
  deviceAdminBinary: '',
  deviceAdminSshHost: '',
  deviceAdminSshDir: '',
  defaultSerialPort: '',
};

describe('parseProvision', () => {
  it('round-trips device id and cloud hash', () => {
    const text = `device ${DEVICE}\ncloud-sha256 ${HASH}\n`;
    expect(parseProvision(text)).toEqual({ deviceId: DEVICE, claimHash: HASH });
  });

  it('returns null when either line is missing', () => {
    expect(parseProvision(`device ${DEVICE}\n`)).toBeNull();
    expect(parseProvision('')).toBeNull();
  });
});

describe('buildDeviceAdminArgs', () => {
  it('runs the CLI through go run', () => {
    expect(buildDeviceAdminArgs(['status', '-device', DEVICE])).toEqual([
      'go',
      'run',
      './cmd/device-admin',
      'status',
      '-device',
      DEVICE,
    ]);
  });
});

describe('deviceAdminInvocation modes', () => {
  it('defaults to go run from the ingestion-service directory', () => {
    const invocation = deviceAdminInvocation(baseConfig, ['status', '-device', DEVICE]);
    expect(invocation.command).toEqual(['go', 'run', './cmd/device-admin', 'status', '-device', DEVICE]);
    expect(invocation.options.env?.DATABASE_URL).toBe(baseConfig.databaseUrl);
  });

  it('runs a configured binary', () => {
    const invocation = deviceAdminInvocation(
      { ...baseConfig, deviceAdminMode: 'binary', deviceAdminBinary: 'C:/bin/device-admin.exe' },
      ['list', '-json'],
    );
    expect(invocation.command).toEqual(['C:/bin/device-admin.exe', 'list', '-json']);
  });

  it('runs over ssh with a quoted remote command', () => {
    const invocation = deviceAdminInvocation(
      {
        ...baseConfig,
        deviceAdminMode: 'ssh',
        deviceAdminSshHost: 'user@vps',
        deviceAdminSshDir: '/srv/checkpoint/ingestion-service',
      },
      ['status', '-device', DEVICE],
    );
    expect(invocation.command.slice(0, 2)).toEqual(['ssh', 'user@vps']);
    expect(invocation.command[2]).toBe(
      `cd /srv/checkpoint/ingestion-service && ./device-admin 'status' '-device' '${DEVICE}'`,
    );
  });
});

describe('shellQuote', () => {
  it('escapes single quotes', () => {
    expect(shellQuote("a'b")).toBe(`'a'\\''b'`);
  });
});
