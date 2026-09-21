import {
  DEVICE_NAME,
  resolveDeveloperMode,
  TRANSFER_RETENTION_HOURS,
  VAD_MIN_SPEECH_S_DEFAULT,
  VAD_THRESHOLD_DEFAULT,
} from './config.ts';

import { env } from '@/lib/env';
import { getDevHost, loadServerConfig, resolveApiUrl } from '@/lib/server-config';
import { storage } from '@/lib/storage';
import { prefKey, settingsKey } from '@/lib/storage/keys';

export interface CheckpointSettings {
  serverUrl: string;
  deviceName: string;
  vadThreshold: number;
  minSpeechS: number;
  vadEnabled: boolean;
  autoSyncEnabled: boolean;
  remindersEnabled: boolean;
  retentionHours: number;
  developerMode: boolean;
}

const KEYS = {
  serverUrl: settingsKey('serverUrl'),
  deviceName: settingsKey('deviceName'),
  vadThreshold: settingsKey('vadThreshold'),
  minSpeechS: settingsKey('minSpeechS'),
  vadEnabled: settingsKey('vadEnabled'),
  autoSyncEnabled: settingsKey('autoSyncEnabled'),
  remindersEnabled: settingsKey('remindersEnabled'),
  retentionHours: settingsKey('retentionHours'),
  developerMode: settingsKey('developerMode'),
} as const;

// One-time migration to the current VAD defaults. Runs once per install;
// a field is only rewritten when it still holds the previous default, so
// values the user tuned themselves are left alone.
const VAD_DEFAULTS_VERSION_KEY = prefKey('vadDefaultsV2');
const PREVIOUS_VAD_THRESHOLD = 0.85;
const PREVIOUS_VAD_MIN_SPEECH_S = 1.5;

async function migrateVadDefaults(): Promise<void> {
  if ((await storage.get(VAD_DEFAULTS_VERSION_KEY)) === '1') return;
  const [threshold, minSpeechS] = await Promise.all([
    storage.get(KEYS.vadThreshold),
    storage.get(KEYS.minSpeechS),
  ]);
  await Promise.all([
    threshold !== null && Number(threshold) === PREVIOUS_VAD_THRESHOLD
      ? storage.set(KEYS.vadThreshold, String(VAD_THRESHOLD_DEFAULT))
      : Promise.resolve(),
    minSpeechS !== null && Number(minSpeechS) === PREVIOUS_VAD_MIN_SPEECH_S
      ? storage.set(KEYS.minSpeechS, String(VAD_MIN_SPEECH_S_DEFAULT))
      : Promise.resolve(),
    storage.set(VAD_DEFAULTS_VERSION_KEY, '1'),
  ]);
}

export function defaultSettings(): CheckpointSettings {
  return {
    serverUrl: resolveApiUrl(),
    deviceName: DEVICE_NAME,
    vadThreshold: VAD_THRESHOLD_DEFAULT,
    minSpeechS: VAD_MIN_SPEECH_S_DEFAULT,
    vadEnabled: true,
    autoSyncEnabled: true,
    remindersEnabled: true,
    retentionHours: TRANSFER_RETENTION_HOURS,
    developerMode: env.devBuild,
  };
}

function toNumber(raw: string | null, fallback: number): number {
  if (raw === null) return fallback;
  const value = Number(raw);
  return Number.isFinite(value) ? value : fallback;
}

export async function loadSettings(): Promise<CheckpointSettings> {
  await loadServerConfig();
  await migrateVadDefaults();
  const defaults = defaultSettings();
  const [
    serverUrl,
    deviceName,
    vadThreshold,
    minSpeechS,
    vadEnabled,
    autoSyncEnabled,
    remindersEnabled,
    retentionHours,
    developerMode,
  ] = await Promise.all([
    storage.get(KEYS.serverUrl),
    storage.get(KEYS.deviceName),
    storage.get(KEYS.vadThreshold),
    storage.get(KEYS.minSpeechS),
    storage.get(KEYS.vadEnabled),
    storage.get(KEYS.autoSyncEnabled),
    storage.get(KEYS.remindersEnabled),
    storage.get(KEYS.retentionHours),
    storage.get(KEYS.developerMode),
  ]);
  const savedServerUrl = (serverUrl ?? '').trim();
  const effectiveServerUrl = getDevHost()
    ? defaults.serverUrl
    : savedServerUrl || defaults.serverUrl;
  return {
    serverUrl: effectiveServerUrl,
    deviceName: (deviceName ?? '').trim() || defaults.deviceName,
    vadThreshold: toNumber(vadThreshold, defaults.vadThreshold),
    minSpeechS: toNumber(minSpeechS, defaults.minSpeechS),
    vadEnabled: vadEnabled !== '0',
    autoSyncEnabled: autoSyncEnabled !== '0',
    remindersEnabled: remindersEnabled !== '0',
    retentionHours: toNumber(retentionHours, defaults.retentionHours),
    developerMode: resolveDeveloperMode(developerMode, env.devBuild),
  };
}

/** Null when the user never made a choice (fresh install, pre-default era). */
export async function loadReminderChoice(): Promise<boolean | null> {
  const raw = await storage.get(KEYS.remindersEnabled);
  if (raw === null) return null;
  return raw === '1';
}

export async function saveReminderChoice(enabled: boolean): Promise<void> {
  await storage.set(KEYS.remindersEnabled, enabled ? '1' : '0');
}

export async function saveSettings(settings: CheckpointSettings): Promise<void> {
  await Promise.all([
    storage.set(KEYS.serverUrl, settings.serverUrl.trim()),
    storage.set(KEYS.deviceName, settings.deviceName.trim()),
    storage.set(KEYS.vadThreshold, String(settings.vadThreshold)),
    storage.set(KEYS.minSpeechS, String(settings.minSpeechS)),
    storage.set(KEYS.vadEnabled, settings.vadEnabled ? '1' : '0'),
    storage.set(KEYS.autoSyncEnabled, settings.autoSyncEnabled ? '1' : '0'),
    storage.set(KEYS.remindersEnabled, settings.remindersEnabled ? '1' : '0'),
    storage.set(KEYS.retentionHours, String(settings.retentionHours)),
    storage.set(KEYS.developerMode, settings.developerMode ? '1' : '0'),
  ]);
}
