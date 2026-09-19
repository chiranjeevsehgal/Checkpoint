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
import { settingsKey } from '@/lib/storage/keys';

export interface CheckpointSettings {
  serverUrl: string;
  deviceName: string;
  vadThreshold: number;
  minSpeechS: number;
  ingestEnabled: boolean;
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
  ingestEnabled: settingsKey('ingestEnabled'),
  vadEnabled: settingsKey('vadEnabled'),
  autoSyncEnabled: settingsKey('autoSyncEnabled'),
  remindersEnabled: settingsKey('remindersEnabled'),
  retentionHours: settingsKey('retentionHours'),
  developerMode: settingsKey('developerMode'),
} as const;

export function defaultSettings(): CheckpointSettings {
  return {
    serverUrl: resolveApiUrl(),
    deviceName: DEVICE_NAME,
    vadThreshold: VAD_THRESHOLD_DEFAULT,
    minSpeechS: VAD_MIN_SPEECH_S_DEFAULT,
    ingestEnabled: true,
    vadEnabled: true,
    autoSyncEnabled: true,
    remindersEnabled: false,
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
  const defaults = defaultSettings();
  const [
    serverUrl,
    deviceName,
    vadThreshold,
    minSpeechS,
    ingestEnabled,
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
    storage.get(KEYS.ingestEnabled),
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
    ingestEnabled: ingestEnabled !== '0',
    vadEnabled: vadEnabled !== '0',
    autoSyncEnabled: autoSyncEnabled !== '0',
    remindersEnabled: remindersEnabled === '1',
    retentionHours: toNumber(retentionHours, defaults.retentionHours),
    developerMode: resolveDeveloperMode(developerMode, env.devBuild),
  };
}

export async function saveSettings(settings: CheckpointSettings): Promise<void> {
  await Promise.all([
    storage.set(KEYS.serverUrl, settings.serverUrl.trim()),
    storage.set(KEYS.deviceName, settings.deviceName.trim()),
    storage.set(KEYS.vadThreshold, String(settings.vadThreshold)),
    storage.set(KEYS.minSpeechS, String(settings.minSpeechS)),
    storage.set(KEYS.ingestEnabled, settings.ingestEnabled ? '1' : '0'),
    storage.set(KEYS.vadEnabled, settings.vadEnabled ? '1' : '0'),
    storage.set(KEYS.autoSyncEnabled, settings.autoSyncEnabled ? '1' : '0'),
    storage.set(KEYS.remindersEnabled, settings.remindersEnabled ? '1' : '0'),
    storage.set(KEYS.retentionHours, String(settings.retentionHours)),
    storage.set(KEYS.developerMode, settings.developerMode ? '1' : '0'),
  ]);
}
