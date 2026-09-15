import { describe, expect, it } from 'vitest';

import { buildDeviceAdminArgs, parseProvision } from '../src/deviceAdmin';

const DEVICE = '474f8bcaff162d3f18fbda36a168f201';
const HASH = 'ab'.repeat(32);

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
