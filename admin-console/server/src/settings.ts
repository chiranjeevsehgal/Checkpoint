import { createCipheriv, createDecipheriv, randomBytes, scryptSync } from 'node:crypto';
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

// At-rest encryption for settings.json. The key comes from ADMIN_SETTINGS_KEY
// (a passphrase; never stored in settings.json itself). When the key is set,
// files are written encrypted; reads transparently handle encrypted and
// legacy plaintext files. Without the key, an encrypted file reads as empty
// so secrets are never silently exposed.
function settingsKey(): Buffer | null {
  const secret = (process.env.ADMIN_SETTINGS_KEY ?? '').trim();
  if (!secret) return null;
  return scryptSync(secret, 'checkpoint-admin-settings', 32);
}

interface EncryptedFile {
  enc: string;
}

function isEncryptedFile(value: unknown): value is EncryptedFile {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as Record<string, unknown>).enc === 'string'
  );
}

function decryptSettings(payload: string, key: Buffer): Settings {
  const raw = Buffer.from(payload, 'base64');
  const iv = raw.subarray(0, 12);
  const tag = raw.subarray(raw.length - 16);
  const ciphertext = raw.subarray(12, raw.length - 16);
  const decipher = createDecipheriv('aes-256-gcm', key, iv);
  decipher.setAuthTag(tag);
  const plain = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return pickSettings(JSON.parse(plain.toString('utf8')) as Record<string, unknown>);
}

export function readSettings(repoRoot: string): Settings {
  const path = settingsPath(repoRoot);
  if (!existsSync(path)) return {};
  try {
    const parsed: unknown = JSON.parse(readFileSync(path, 'utf8'));
    if (isEncryptedFile(parsed)) {
      const key = settingsKey();
      if (!key) {
        console.warn('settings.json is encrypted but ADMIN_SETTINGS_KEY is not set; ignoring it');
        return {};
      }
      return decryptSettings(parsed.enc, key);
    }
    return pickSettings(parsed as Record<string, unknown>);
  } catch {
    return {};
  }
}

export function writeSettings(repoRoot: string, values: Settings): void {
  const path = settingsPath(repoRoot);
  mkdirSync(dirname(path), { recursive: true });
  const key = settingsKey();
  const body =
    key === null
      ? `${JSON.stringify(values, null, 2)}\n`
      : `${JSON.stringify({ enc: encryptSettings(values, key) })}\n`;
  const temp = `${path}.tmp`;
  writeFileSync(temp, body, 'utf8');
  renameSync(temp, path);
}

function encryptSettings(values: Settings, key: Buffer): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', key, iv);
  const ciphertext = Buffer.concat([
    cipher.update(JSON.stringify(values), 'utf8'),
    cipher.final(),
  ]);
  const tag = cipher.getAuthTag();
  return Buffer.concat([iv, ciphertext, tag]).toString('base64');
}

export function pickSettings(values: Record<string, unknown>): Settings {
  const settings: Settings = {};
  for (const key of SETTINGS_KEYS) {
    const value = values[key];
    if (typeof value === 'string' && value.trim()) settings[key] = value.trim();
  }
  return settings;
}
