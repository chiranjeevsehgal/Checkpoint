import { storage } from "@/lib/storage";
import { env } from "@/lib/env";

import {
  INGEST_USER_ID_DEFAULT,
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
}

const KEYS = {
  serverUrl: "checkpoint/settings/serverUrl",
  userId: "checkpoint/settings/userId",
  vadThreshold: "checkpoint/settings/vadThreshold",
  minSpeechS: "checkpoint/settings/minSpeechS",
  keepFiles: "checkpoint/settings/keepFiles",
  ingestEnabled: "checkpoint/settings/ingestEnabled",
  vadEnabled: "checkpoint/settings/vadEnabled",
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
  };
}

function toNumber(raw: string | null, fallback: number): number {
  if (raw === null) return fallback;
  const value = Number(raw);
  return Number.isFinite(value) ? value : fallback;
}

export async function loadSettings(): Promise<CheckpointSettings> {
  const defaults = defaultSettings();
  const [serverUrl, userId, vadThreshold, minSpeechS, keepFiles, ingestEnabled, vadEnabled] =
    await Promise.all([
      storage.get(KEYS.serverUrl),
      storage.get(KEYS.userId),
      storage.get(KEYS.vadThreshold),
      storage.get(KEYS.minSpeechS),
      storage.get(KEYS.keepFiles),
      storage.get(KEYS.ingestEnabled),
      storage.get(KEYS.vadEnabled),
    ]);
  return {
    serverUrl: (serverUrl ?? "").trim() || defaults.serverUrl,
    userId: (userId ?? "").trim() || defaults.userId,
    vadThreshold: toNumber(vadThreshold, defaults.vadThreshold),
    minSpeechS: toNumber(minSpeechS, defaults.minSpeechS),
    keepFiles: keepFiles === "1",
    ingestEnabled: ingestEnabled !== "0",
    vadEnabled: vadEnabled !== "0",
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
  ]);
}
