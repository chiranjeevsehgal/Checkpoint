import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

// Settings are stored with the same ADMIN_* names the agent already reads, so a
// value set here is indistinguishable from one set in a .env file.
export const SETTINGS_KEYS = [
  'ADMIN_DATABASE_URL',
  'ADMIN_KRATOS_ADMIN_URL',
  'ADMIN_KRATOS_PUBLIC_URL',
  'ADMIN_INGESTION_URL',
  'ADMIN_DOCKER_HOST',
  'ADMIN_ARDUINO_CLI',
  'ADMIN_SKETCH_DIR',
  'ADMIN_BUILD_DIR',
  'ADMIN_FQBN',
  'ADMIN_DEVICE_ADMIN_MODE',
  'ADMIN_DEVICE_ADMIN_BINARY',
  'ADMIN_DEVICE_ADMIN_SSH_HOST',
  'ADMIN_DEVICE_ADMIN_SSH_DIR',
  'ADMIN_DEFAULT_SERIAL_PORT',
] as const;

export type SettingsKey = (typeof SETTINGS_KEYS)[number];
export type Settings = Partial<Record<SettingsKey, string>>;

export function settingsPath(repoRoot: string): string {
  return resolve(repoRoot, 'admin-console', 'server', 'data', 'settings.json');
}

export function readSettings(repoRoot: string): Settings {
  const path = settingsPath(repoRoot);
  if (!existsSync(path)) return {};
  try {
    return pickSettings(JSON.parse(readFileSync(path, 'utf8')) as Record<string, unknown>);
  } catch {
    return {};
  }
}

export function writeSettings(repoRoot: string, values: Settings): void {
  const path = settingsPath(repoRoot);
  mkdirSync(dirname(path), { recursive: true });
  const temp = `${path}.tmp`;
  writeFileSync(temp, `${JSON.stringify(values, null, 2)}\n`, 'utf8');
  renameSync(temp, path);
}

export function pickSettings(values: Record<string, unknown>): Settings {
  const settings: Settings = {};
  for (const key of SETTINGS_KEYS) {
    const value = values[key];
    if (typeof value === 'string' && value.trim()) settings[key] = value.trim();
  }
  return settings;
}
