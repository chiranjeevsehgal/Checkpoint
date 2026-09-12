import { storage } from "@/lib/storage";
import { settingsKey } from "@/lib/storage/keys";
import { env } from "@/lib/env";

import {
  INGEST_USER_ID_DEFAULT,
  TRANSFER_RETENTION_HOURS,
  VAD_MIN_SPEECH_S_DEFAULT,
  VAD_THRESHOLD_DEFAULT,
} from "./config.ts";

export interface CheckpointSettings {
  serverUrl: string;
  userId: string;
  vadThreshold: number;
  minSpeechS: number;
  keepFiles: boolean;
  ingestEnabled: boolean;
  vadEnabled: boolean;
  autoSyncEnabled: boolean;
  retentionHours: number;
}

const KEYS = {
  serverUrl: settingsKey("serverUrl"),
  userId: settingsKey("userId"),
  vadThreshold: settingsKey("vadThreshold"),
  minSpeechS: settingsKey("minSpeechS"),
  keepFiles: settingsKey("keepFiles"),
  ingestEnabled: settingsKey("ingestEnabled"),
  vadEnabled: settingsKey("vadEnabled"),
  autoSyncEnabled: settingsKey("autoSyncEnabled"),
  retentionHours: settingsKey("retentionHours"),
} as const;

export function defaultSettings(): CheckpointSettings {
  return {
    serverUrl: env.apiUrl,
    userId: INGEST_USER_ID_DEFAULT,
    vadThreshold: VAD_THRESHOLD_DEFAULT,
    minSpeechS: VAD_MIN_SPEECH_S_DEFAULT,
    keepFiles: false,
    ingestEnabled: true,
    vadEnabled: true,
    autoSyncEnabled: true,
    retentionHours: TRANSFER_RETENTION_HOURS,
  };
}

function toNumber(raw: string | null, fallback: number): number {
  if (raw === null) return fallback;
  const value = Number(raw);
  return Number.isFinite(value) ? value : fallback;
}

export async function loadSettings(): Promise<CheckpointSettings> {
  const defaults = defaultSettings();
  const [
    serverUrl,
    userId,
    vadThreshold,
    minSpeechS,
    keepFiles,
    ingestEnabled,
    vadEnabled,
    autoSyncEnabled,
    retentionHours,
  ] = await Promise.all([
    storage.get(KEYS.serverUrl),
    storage.get(KEYS.userId),
    storage.get(KEYS.vadThreshold),
    storage.get(KEYS.minSpeechS),
    storage.get(KEYS.keepFiles),
    storage.get(KEYS.ingestEnabled),
    storage.get(KEYS.vadEnabled),
    storage.get(KEYS.autoSyncEnabled),
    storage.get(KEYS.retentionHours),
  ]);
  return {
    serverUrl: (serverUrl ?? "").trim() || defaults.serverUrl,
    userId: (userId ?? "").trim() || defaults.userId,
    vadThreshold: toNumber(vadThreshold, defaults.vadThreshold),
    minSpeechS: toNumber(minSpeechS, defaults.minSpeechS),
    keepFiles: keepFiles === "1",
    ingestEnabled: ingestEnabled !== "0",
    vadEnabled: vadEnabled !== "0",
    autoSyncEnabled: autoSyncEnabled !== "0",
    retentionHours: toNumber(retentionHours, defaults.retentionHours),
  };
}

export async function saveSettings(
  settings: CheckpointSettings,
): Promise<void> {
  await Promise.all([
    storage.set(KEYS.serverUrl, settings.serverUrl.trim()),
    storage.set(KEYS.userId, settings.userId.trim()),
    storage.set(KEYS.vadThreshold, String(settings.vadThreshold)),
    storage.set(KEYS.minSpeechS, String(settings.minSpeechS)),
    storage.set(KEYS.keepFiles, settings.keepFiles ? "1" : "0"),
    storage.set(KEYS.ingestEnabled, settings.ingestEnabled ? "1" : "0"),
    storage.set(KEYS.vadEnabled, settings.vadEnabled ? "1" : "0"),
    storage.set(KEYS.autoSyncEnabled, settings.autoSyncEnabled ? "1" : "0"),
    storage.set(KEYS.retentionHours, String(settings.retentionHours)),
  ]);
}
