export type ActivityTone = 'recording' | 'capturing' | 'syncing' | 'connected' | 'disconnected';

export interface ConnectionActivity {
  label: string;
  tone: ActivityTone;
}

export type SignalLevel = 0 | 1 | 2 | 3 | 4;

/** Buckets a Bluetooth RSSI reading (dBm) into a five-step signal level. */
export function signalLevel(dbm: number): SignalLevel {
  if (dbm >= -50) return 4;
  if (dbm >= -70) return 3;
  if (dbm >= -85) return 2;
  if (dbm >= -95) return 1;
  return 0;
}

const SIGNAL_LABELS: Record<SignalLevel, string> = {
  4: 'Excellent',
  3: 'Good',
  2: 'Fair',
  1: 'Weak',
  0: 'Very weak',
};

export function signalLabel(level: SignalLevel): string {
  return SIGNAL_LABELS[level];
}

/** Pendant mic silence floor; mirrors firmware VAD_ABS_FLOOR_DBFS (config.h:78). */
export const VAD_SILENCE_FLOOR_DBFS = -54;

/** Maps a pendant dBFS level to 0..100 with the silence floor at 0. */
export function levelPercent(dbfs: number): number {
  const clamped = Math.max(VAD_SILENCE_FLOOR_DBFS, Math.min(0, dbfs));
  return ((clamped - VAD_SILENCE_FLOOR_DBFS) / -VAD_SILENCE_FLOOR_DBFS) * 100;
}

export function formatFingerprint(deviceIdHex: string | null): string {
  if (!deviceIdHex || deviceIdHex.length < 8) return '';
  const hex = deviceIdHex.toUpperCase();
  return `${hex.slice(0, 4)}-${hex.slice(4, 8)}`;
}

export function connectionActivity(input: {
  connected: boolean;
  recording: boolean;
  vadActive: boolean;
  syncing: number;
}): ConnectionActivity {
  if (!input.connected) return { label: 'Not connected', tone: 'disconnected' };
  if (input.recording) return { label: 'Recording now', tone: 'recording' };
  if (input.vadActive) return { label: 'Capturing speech', tone: 'capturing' };
  if (input.syncing > 0) return { label: `Syncing ${input.syncing}`, tone: 'syncing' };
  return { label: 'Idle', tone: 'connected' };
}
