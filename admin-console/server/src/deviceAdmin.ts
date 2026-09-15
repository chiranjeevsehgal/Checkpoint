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

// How the privileged CLI runs: locally via go run, a prebuilt binary, or over
// SSH against a remote host that already has the binary and its DATABASE_URL.
export function deviceAdminInvocation(config: AppConfig, args: string[]): DeviceAdminInvocation {
  if (config.deviceAdminMode === 'binary' && config.deviceAdminBinary) {
    return { command: [config.deviceAdminBinary, ...args], options: { env: databaseEnv(config) } };
  }
  if (config.deviceAdminMode === 'ssh' && config.deviceAdminSshHost && config.deviceAdminSshDir) {
    const remote = `cd ${config.deviceAdminSshDir} && ./device-admin ${args.map(shellQuote).join(' ')}`;
    return { command: ['ssh', config.deviceAdminSshHost, remote], options: {} };
  }
  return {
    command: buildDeviceAdminArgs(args),
    options: { cwd: resolve(config.repoRoot, 'ingestion-service'), env: databaseEnv(config) },
  };
}

export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function databaseEnv(config: AppConfig): NodeJS.ProcessEnv {
  return { ...process.env, DATABASE_URL: config.databaseUrl };
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
