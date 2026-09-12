export type ActivityTone = "live" | "idle";

export interface ConnectionActivity {
  label: string;
  tone: ActivityTone;
}

export function formatFingerprint(deviceIdHex: string | null): string {
  if (!deviceIdHex || deviceIdHex.length < 8) return "";
  const hex = deviceIdHex.toUpperCase();
  return `${hex.slice(0, 4)}-${hex.slice(4, 8)}`;
}

export function connectionActivity(input: {
  connected: boolean;
  recording: boolean;
  vadActive: boolean;
  syncing: number;
}): ConnectionActivity {
  if (!input.connected) return { label: "Not connected", tone: "idle" };
  if (input.recording) return { label: "Recording now", tone: "live" };
  if (input.vadActive) return { label: "Capturing speech", tone: "live" };
  if (input.syncing > 0) return { label: `Syncing ${input.syncing}`, tone: "live" };
  return { label: "Idle · listening", tone: "idle" };
}
