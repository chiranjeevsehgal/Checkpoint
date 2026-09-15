import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

import { FQBN_COMPILE } from './arduino';
import { readSettings } from './settings';

export type DeviceAdminMode = 'go' | 'binary' | 'ssh';

export interface AppConfig {
  host: string;
  port: number;
  repoRoot: string;
  databaseUrl: string;
  kratosAdminUrl: string;
  kratosPublicUrl: string;
  ingestionUrl: string;
  arduinoCli: string;
  sketchDir: string;
  buildDir: string;
  fqbn: string;
  dockerHost: string;
  deviceAdminMode: DeviceAdminMode;
  deviceAdminBinary: string;
  deviceAdminSshHost: string;
  deviceAdminSshDir: string;
  defaultSerialPort: string;
}

export interface ResolvedEnv {
  env: Env;
  sources: Record<string, string>;
  repoRoot: string;
}

export type Env = Record<string, string | undefined>;

export function parseEnvFile(path: string): Env {
  if (!existsSync(path)) return {};
  const values: Env = {};
  for (const raw of readFileSync(path, 'utf8').split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith('#') || !line.includes('=')) continue;
    const index = line.indexOf('=');
    values[line.slice(0, index).trim()] = line.slice(index + 1).trim();
  }
  return values;
}

// Walk up until the directory containing the device-admin CLI is found.
export function findRepoRoot(start: string): string {
  let current = resolve(start);
  for (;;) {
    if (existsSync(resolve(current, 'ingestion-service', 'cmd', 'device-admin'))) {
      return current;
    }
    const parent = dirname(current);
    if (parent === current) return resolve(start);
    current = parent;
  }
}

// Precedence: real environment > settings.json (UI) > .env files > defaults.
export function resolveEnv(start = process.cwd()): ResolvedEnv {
  const repoRoot = findRepoRoot(start);
  const repoEnv = parseEnvFile(resolve(repoRoot, '.env'));
  const consoleEnv = parseEnvFile(resolve(repoRoot, 'admin-console', '.env'));
  const settings = readSettings(repoRoot);
  const env: Env = { ...repoEnv, ...consoleEnv, ...settings, ...process.env };

  const sources: Record<string, string> = {};
  const keys = new Set([
    ...Object.keys(repoEnv),
    ...Object.keys(consoleEnv),
    ...Object.keys(settings),
    ...Object.keys(process.env),
  ]);
  const settingsEnv = settings as Env;
  for (const key of keys) {
    if (process.env[key] !== undefined) sources[key] = 'environment';
    else if (settingsEnv[key] !== undefined) sources[key] = 'settings';
    else if (consoleEnv[key] !== undefined) sources[key] = 'admin-console/.env';
    else if (repoEnv[key] !== undefined) sources[key] = 'repo .env';
  }
  return { env, sources, repoRoot };
}

export function buildDatabaseUrl(values: Env): string {
  const user = (values.POSTGRES_USER ?? '').trim();
  const password = (values.POSTGRES_PASSWORD ?? '').trim();
  const name = (values.POSTGRES_DB ?? '').trim();
  if (!user || !name) return '';
  const credentials = password ? `${user}:${password}` : user;
  return `postgres://${credentials}@localhost:5432/${name}?sslmode=disable`;
}

export function redactDatabaseUrl(url: string): string {
  return url.replace(/:\/\/([^:/@]+):[^@]*@/, '://$1:****@');
}

export function loadConfig(start = process.cwd()): AppConfig {
  const { env, repoRoot } = resolveEnv(start);
  const databaseUrl = (env.ADMIN_DATABASE_URL ?? '').trim() || buildDatabaseUrl(env);
  return {
    host: (env.ADMIN_HOST ?? '').trim() || '127.0.0.1',
    port: toPort(env.ADMIN_PORT),
    repoRoot,
    databaseUrl,
    kratosAdminUrl: (env.ADMIN_KRATOS_ADMIN_URL ?? '').trim() || 'http://127.0.0.1:4434',
    kratosPublicUrl: (env.ADMIN_KRATOS_PUBLIC_URL ?? '').trim() || 'http://127.0.0.1:4433',
    ingestionUrl: (env.ADMIN_INGESTION_URL ?? '').trim() || 'http://127.0.0.1:8080',
    arduinoCli: (env.ADMIN_ARDUINO_CLI ?? '').trim() || 'arduino-cli',
    sketchDir: resolve(repoRoot, (env.ADMIN_SKETCH_DIR ?? '').trim() || 'firmware/checkpoint'),
    buildDir: resolve(repoRoot, (env.ADMIN_BUILD_DIR ?? '').trim() || 'firmware/checkpoint/build'),
    fqbn: (env.ADMIN_FQBN ?? '').trim() || FQBN_COMPILE,
    dockerHost: (env.ADMIN_DOCKER_HOST ?? env.DOCKER_HOST ?? '').trim(),
    deviceAdminMode: toDeviceAdminMode(env.ADMIN_DEVICE_ADMIN_MODE),
    deviceAdminBinary: (env.ADMIN_DEVICE_ADMIN_BINARY ?? '').trim(),
    deviceAdminSshHost: (env.ADMIN_DEVICE_ADMIN_SSH_HOST ?? '').trim(),
    deviceAdminSshDir: (env.ADMIN_DEVICE_ADMIN_SSH_DIR ?? '').trim(),
    defaultSerialPort: (env.ADMIN_DEFAULT_SERIAL_PORT ?? '').trim(),
  };
}

// Re-resolve settings in place so already-registered routes see new values.
export function applyConfig(config: AppConfig, start = process.cwd()): AppConfig {
  Object.assign(config, loadConfig(start));
  return config;
}

function toPort(value: string | undefined): number {
  const port = Number((value ?? '').trim());
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : 4300;
}

function toDeviceAdminMode(value: string | undefined): DeviceAdminMode {
  const mode = (value ?? '').trim();
  return mode === 'binary' || mode === 'ssh' ? mode : 'go';
}
