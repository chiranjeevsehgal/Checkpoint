import { resolve } from 'node:path';

import type { AppConfig } from './config';
import type { RunOptions } from './task';

export interface DeviceAdminInvocation {
  command: string[];
  options: RunOptions;
}

export function buildDeviceAdminArgs(args: string[]): string[] {
  return ['go', 'run', './cmd/device-admin', ...args];
}

export function deviceAdminInvocation(config: AppConfig, args: string[]): DeviceAdminInvocation {
  return {
    command: buildDeviceAdminArgs(args),
    options: {
      cwd: resolve(config.repoRoot, 'ingestion-service'),
      env: { ...process.env, DATABASE_URL: config.databaseUrl },
    },
  };
}

export interface ProvisionIdentity {
  deviceId: string;
  claimHash: string;
}

// Parse the pendant's `auth provision` console output.
export function parseProvision(text: string): ProvisionIdentity | null {
  const device = /^device ([0-9a-f]{32})$/m.exec(text);
  const hash = /^cloud-sha256 ([0-9a-f]{64})$/m.exec(text);
  if (!device || !hash) return null;
  return { deviceId: device[1], claimHash: hash[1] };
}
