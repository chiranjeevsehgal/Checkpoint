import { existsSync, readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { platform } from 'node:process';

import type { FastifyInstance } from 'fastify';

import { applyConfig, buildDatabaseUrl, redactDatabaseUrl, resolveEnv, type AppConfig } from '../config';
import type { AppContext } from '../context';
import { deviceAdminInvocation } from '../deviceAdmin';
import { composeServices } from '../docker';
import { badRequest } from '../httpError';
import { runCapture } from '../process';
import { SETTINGS_KEYS, readSettings, writeSettings, type Settings, type SettingsKey } from '../settings';
import { hasNoWhitespace, isDatabaseUrl, isHttpUrl, isSafeRemotePath } from '../validation';

interface SettingsBody {
  values?: Record<string, unknown>;
}

interface TestBody {
  target?: string;
  value?: string;
}

interface BrowseEntry {
  name: string;
  path: string;
}

interface TestResult {
  ok: boolean;
  detail: string;
}

const URL_KEYS = new Set<SettingsKey>([
  'ADMIN_KRATOS_ADMIN_URL',
  'ADMIN_KRATOS_PUBLIC_URL',
  'ADMIN_INGESTION_URL',
]);

export function registerSettingsRoutes(app: FastifyInstance, context: AppContext): void {
  const { config } = context;

  app.get('/api/settings', () => settingsPayload(config));

  app.put('/api/settings', async (request) => {
    const body = (request.body ?? {}) as SettingsBody;
    const incoming = body.values ?? {};
    const next: Settings = { ...readSettings(config.repoRoot) };

    for (const [key, raw] of Object.entries(incoming)) {
      if (!isSettingsKey(key)) throw badRequest(`unknown setting: ${key}`);
      if (typeof raw !== 'string') throw badRequest(`${key} must be a string`);
      const value = raw.trim();
      if (!value) {
        delete next[key];
        continue;
      }
      validateSetting(key, value);
      next[key] = value;
    }
    validateDeviceAdmin(next);

    writeSettings(config.repoRoot, next);
    applyConfig(config);
    return settingsPayload(config);
  });

  app.get('/api/settings/browse', async (request) => {
    const requested = ((request.query as { path?: string }).path ?? '').trim();
    if (!requested) {
      return { path: '', parent: null, entries: filesystemRoots() };
    }
    const target = resolve(requested);
    if (!existsSync(target)) throw badRequest(`not found: ${target}`);
    try {
      const entries = readdirSync(target, { withFileTypes: true })
        .filter((entry) => entry.isDirectory())
        .map((entry) => ({ name: entry.name, path: join(target, entry.name) }))
        .sort((a, b) => a.name.localeCompare(b.name));
      const parent = resolve(target, '..');
      return { path: target, parent: parent === target ? null : parent, entries };
    } catch (error) {
      throw badRequest(error instanceof Error ? error.message : `cannot read ${target}`);
    }
  });

  app.post('/api/settings/detect-arduino', async () => ({
    candidates: await arduinoCandidates(config.arduinoCli),
  }));

  app.post('/api/settings/test', async (request) => {
    const body = (request.body ?? {}) as TestBody;
    const target = (body.target ?? '').trim();
    const value = (body.value ?? '').trim();
    switch (target) {
      case 'database':
        return testDatabase(config, value);
      case 'kratos':
        return testHttp(value || config.kratosAdminUrl, '/admin/identities?page_size=1');
      case 'ingestion':
        return testHttp(value || config.ingestionUrl, '/health/ready');
      case 'docker':
        return testDocker(config, value);
      default:
        throw badRequest(`unknown test target: ${target}`);
    }
  });
}

function settingsPayload(config: AppConfig): {
  values: Record<string, string>;
  sources: Record<string, string>;
  host: string;
  port: number;
} {
  const { env, sources } = resolveEnv();
  const values: Record<string, string> = {};
  for (const key of SETTINGS_KEYS) {
    const value = (env[key] ?? '').trim();
    if (value) values[key] = value;
  }
  values.ADMIN_DATABASE_URL = redactDatabaseUrl(config.databaseUrl || buildDatabaseUrl(env));
  return {
    values,
    sources: Object.fromEntries(SETTINGS_KEYS.map((key) => [key, sources[key] ?? 'default'])),
    host: config.host,
    port: config.port,
  };
}

function validateSetting(key: SettingsKey, value: string): void {
  if (URL_KEYS.has(key) && !isHttpUrl(value)) {
    throw badRequest(`${key} must be an http(s) URL`);
  }
  if (key === 'ADMIN_DATABASE_URL' && !isDatabaseUrl(value)) {
    throw badRequest('ADMIN_DATABASE_URL must be a postgres:// URL');
  }
  if (key === 'ADMIN_FQBN' && !hasNoWhitespace(value)) {
    throw badRequest('ADMIN_FQBN must not contain whitespace');
  }
  if (key === 'ADMIN_DEVICE_ADMIN_MODE' && !['go', 'binary', 'ssh'].includes(value)) {
    throw badRequest('ADMIN_DEVICE_ADMIN_MODE must be go, binary or ssh');
  }
  if (key === 'ADMIN_DEVICE_ADMIN_SSH_HOST' && !hasNoWhitespace(value)) {
    throw badRequest('ADMIN_DEVICE_ADMIN_SSH_HOST must not contain whitespace');
  }
  if (key === 'ADMIN_DEVICE_ADMIN_SSH_DIR' && !isSafeRemotePath(value)) {
    throw badRequest('ADMIN_DEVICE_ADMIN_SSH_DIR must be a simple remote path');
  }
  if (key !== 'ADMIN_DEVICE_ADMIN_SSH_DIR' && /[\r\n]/.test(value)) {
    throw badRequest(`${key} must be a single line`);
  }
}

function validateDeviceAdmin(settings: Settings): void {
  const mode = settings.ADMIN_DEVICE_ADMIN_MODE ?? 'go';
  if (mode === 'binary' && !settings.ADMIN_DEVICE_ADMIN_BINARY) {
    throw badRequest('binary mode requires ADMIN_DEVICE_ADMIN_BINARY');
  }
  if (mode === 'ssh' && (!settings.ADMIN_DEVICE_ADMIN_SSH_HOST || !settings.ADMIN_DEVICE_ADMIN_SSH_DIR)) {
    throw badRequest('ssh mode requires ADMIN_DEVICE_ADMIN_SSH_HOST and ADMIN_DEVICE_ADMIN_SSH_DIR');
  }
}

function isSettingsKey(value: string): value is SettingsKey {
  return (SETTINGS_KEYS as readonly string[]).includes(value);
}

function filesystemRoots(): BrowseEntry[] {
  if (platform !== 'win32') return [{ name: '/', path: '/' }];
  const roots: BrowseEntry[] = [];
  for (let code = 'A'.charCodeAt(0); code <= 'Z'.charCodeAt(0); code += 1) {
    const letter = String.fromCharCode(code);
    const path = `${letter}:\\`;
    if (existsSync(path)) roots.push({ name: path, path });
  }
  return roots;
}

async function arduinoCandidates(configured: string): Promise<{ path: string; version: string }[]> {
  const paths = new Set<string>();
  const command = platform === 'win32' ? ['where.exe', 'arduino-cli'] : ['which', '-a', 'arduino-cli'];
  const result = await runCapture(command);
  if (result.code === 0) {
    for (const line of result.stdout.split(/\r?\n/)) {
      if (line.trim()) paths.add(line.trim());
    }
  }
  if (configured && existsSync(configured)) paths.add(configured);

  const candidates: { path: string; version: string }[] = [];
  for (const path of paths) {
    candidates.push({ path, version: await arduinoVersion(path) });
  }
  return candidates;
}

async function arduinoVersion(cli: string): Promise<string> {
  try {
    const result = await runCapture([cli, 'version']);
    return result.code === 0 ? firstLine(result.stdout) : '';
  } catch {
    return '';
  }
}

async function testDatabase(config: AppConfig, value: string): Promise<TestResult> {
  const candidate: AppConfig = { ...config, databaseUrl: value || config.databaseUrl };
  if (!candidate.databaseUrl) return { ok: false, detail: 'no database url configured' };
  const invocation = deviceAdminInvocation(candidate, ['list', '-json']);
  const result = await runCapture(invocation.command, invocation.options);
  if (result.code === 0) return { ok: true, detail: 'connected' };
  return { ok: false, detail: firstLine(result.stderr || result.stdout) };
}

async function testHttp(baseUrl: string, path: string): Promise<TestResult> {
  if (!isHttpUrl(baseUrl)) return { ok: false, detail: 'invalid url' };
  try {
    const response = await fetch(new URL(path, baseUrl), { signal: AbortSignal.timeout(5000) });
    return { ok: response.ok, detail: `HTTP ${response.status}` };
  } catch (error) {
    return { ok: false, detail: error instanceof Error ? error.message : 'unreachable' };
  }
}

async function testDocker(config: AppConfig, value: string): Promise<TestResult> {
  try {
    const services = await composeServices({ ...config, dockerHost: value || config.dockerHost });
    return { ok: true, detail: `${services.length} service(s)` };
  } catch (error) {
    return { ok: false, detail: error instanceof Error ? error.message : 'docker unreachable' };
  }
}

function firstLine(text: string): string {
  return text.trim().split(/\r?\n/)[0] ?? '';
}
